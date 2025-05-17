package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/judwhite/go-svc"
	"github.com/mreiferson/go-options"

	"github.com/nsqio/nsq/internal/lg"
	"github.com/nsqio/nsq/internal/version"
	"github.com/nsqio/nsq/nsqd"
)

// program 实现了三个接口: Service, Handler, Context
type program struct {
	// once 保证 Stop 只执行一次
	once sync.Once
	nsqd *nsqd.NSQD
}

func main() {
	// 实例化服务主程序，该结构体实现了 svc.Service 接口
	prg := &program{}

	// 启动服务管理框架，监听 SIGINT(Ctrl+C) 和 SIGTERM(终止信号)
	// svc.Run 执行流程:
	// 1. 调用 Init 进行初始化（配置加载、组件实例化）
	// 2. 调用 Start 启动服务（加载元数据、持久化存储、主循环）
	// 3. 阻塞等待信号，收到信号后调用 Stop 进行优雅退出
	if err := svc.Run(prg, syscall.SIGINT, syscall.SIGTERM); err != nil {
		// 遇到致命错误时记录日志并退出，返回非零状态码
		logFatal("%s", err)
	}
}

func (p *program) Init(env svc.Environment) error {
	// 创建默认配置选项
	opts := nsqd.NewOptions()

	// 初始化命令行参数集并解析
	flagSet := nsqdFlagSet(opts)
	flagSet.Parse(os.Args[1:])

	rand.Seed(time.Now().UTC().UnixNano())

	// 处理版本查询参数 --version
	if flagSet.Lookup("version").Value.(flag.Getter).Get().(bool) {
		fmt.Println(version.String("nsqd"))
		os.Exit(0) // 输出版本信息后直接退出
	}

	var cfg config
	// 获取配置文件路径参数 --config
	configFile := flagSet.Lookup("config").Value.String()
	if configFile != "" {
		// 加载并解析TOML格式的配置文件
		_, err := toml.DecodeFile(configFile, &cfg)
		if err != nil {
			logFatal("failed to load config file %s - %s", configFile, err)
		}
	}
	cfg.Validate()

	// 配置来自三个地方: 默认配置、命令行参数、配置文件
	// Resolve 会将这三个地方的配置合并到 opts 中, 函数里有优先级的说明
	options.Resolve(opts, flagSet, cfg)

	nsqd, err := nsqd.New(opts)
	if err != nil {
		logFatal("failed to instantiate nsqd - %s", err)
	}
	p.nsqd = nsqd

	return nil
}

func (p *program) Start() error {
	err := p.nsqd.LoadMetadata()
	if err != nil {
		logFatal("failed to load metadata - %s", err)
	}
	err = p.nsqd.PersistMetadata()
	if err != nil {
		logFatal("failed to persist metadata - %s", err)
	}

	go func() {
		err := p.nsqd.Main()
		if err != nil {
			p.Stop()
			os.Exit(1)
		}
	}()

	return nil
}

func (p *program) Stop() error {
	p.once.Do(func() {
		p.nsqd.Exit()
	})
	return nil
}

func (p *program) Handle(s os.Signal) error {
	return svc.ErrStop
}

// Context returns a context that will be canceled when nsqd initiates the shutdown
func (p *program) Context() context.Context {
	return p.nsqd.Context()
}

func logFatal(f string, args ...interface{}) {
	lg.LogFatal("[nsqd] ", f, args...)
}
