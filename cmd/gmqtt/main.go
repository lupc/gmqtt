package main

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"go.uber.org/zap"

	"github.com/DrmagicE/gmqtt/config"
	_ "github.com/DrmagicE/gmqtt/persistence"
	"github.com/DrmagicE/gmqtt/pkg/pidfile"
	_ "github.com/DrmagicE/gmqtt/plugin/prometheus"
	"github.com/DrmagicE/gmqtt/server"
	_ "github.com/DrmagicE/gmqtt/topicalias/fifo"
	"github.com/kardianos/service"
)

var (
	enablePprof bool
	pprofAddr   = "127.0.0.1:6060"
	ConfigFile  string
	ConfigFile2 string
	logger      *zap.Logger
)

func must(err error) {
	if err != nil {
		if logger != nil {
			logger.Error(err.Error())
		} else {
			fmt.Fprint(os.Stderr, err.Error())
		}

		os.Exit(1)
	}
}

// Chdir 将程序工作路径修改成程序所在位置
func Chdir() (err error) {
	dir, err := filepath.Abs(filepath.Dir(os.Args[0]))
	if err != nil {
		return
	}
	err = os.Chdir(dir)
	// workDir, _ := filepath.Abs("")
	// logger.Sugar().Infof("工作目录：%v", workDir)
	return
}

func init() {

	ConfigFile = "config.yml"
	ConfigFile2 = "config.yaml"

}

func main() {
	srvConfig := &service.Config{
		Name:        "gmqtt",
		DisplayName: "gmqtt MQTT Broker 服务",
		Description: "gmqtt MQTT Broker 服务",
	}
	prg := &program{}
	prg.RunFn = run
	// var err error
	var s, err = service.New(prg, srvConfig)
	if err != nil {
		fmt.Printf("创建服务出错：%v", err)
	}
	var name = fmt.Sprintf("服务[%v]", srvConfig.Name)
	if len(os.Args) > 1 {
		serviceAction := os.Args[1]
		switch serviceAction {
		case "install":
			err := s.Install()
			if err != nil {
				fmt.Println(fmt.Sprintf("安装%v失败: ", name), err.Error())
			} else {
				fmt.Println(fmt.Sprintf("安装%v成功", name))
			}
		case "uninstall":
			err := s.Uninstall()
			if err != nil {
				fmt.Println(fmt.Sprintf("卸载%v失败: ", name), err.Error())
			} else {
				fmt.Println(fmt.Sprintf("卸载%v成功", name))
			}
		case "start":
			err := s.Start()
			if err != nil {
				fmt.Println(fmt.Sprintf("运行%v失败: ", name), err.Error())
			} else {
				fmt.Println(fmt.Sprintf("运行%v成功", name))
			}
		case "stop":
			err := s.Stop()
			if err != nil {
				fmt.Println(fmt.Sprintf("停止%v失败: ", name), err.Error())
			} else {
				fmt.Println(fmt.Sprintf("停止%v成功", name))
			}
		}
		return
	}

	//不带参数直接运行
	err = s.Run()
	if err != nil {
		fmt.Println(err)
	}
}

type program struct {
	RunFn func() //运行方法
}

func (p *program) Start(s service.Service) error {
	fmt.Print("服务运行...")
	go p.RunFn()
	return nil
}

func (p *program) Stop(s service.Service) error {
	fmt.Print("服务停止。")
	return nil
}

func GetListeners(c config.Config) (tcpListeners []net.Listener, websockets []*server.WsServer, err error) {
	for _, v := range c.Listeners {
		var ln net.Listener
		if v.Websocket != nil {
			ws := &server.WsServer{
				Server: &http.Server{Addr: v.Address},
				Path:   v.Websocket.Path,
			}
			if v.TLSOptions != nil {
				ws.KeyFile = v.Key
				ws.CertFile = v.Cert
			}
			websockets = append(websockets, ws)
			continue
		}
		if v.TLSOptions != nil {
			var cert tls.Certificate
			cert, err = tls.LoadX509KeyPair(v.Cert, v.Key)
			if err != nil {
				return
			}
			ln, err = tls.Listen("tcp", v.Address, &tls.Config{
				Certificates: []tls.Certificate{cert},
			})
		} else {
			ln, err = net.Listen("tcp", v.Address)
		}
		tcpListeners = append(tcpListeners, ln)
	}
	return
}

// func loadConfig() config.Config {
// 	c, err := config.ParseConfig(ConfigFile)
// 	if os.IsNotExist(err) {
// 		c, err = config.ParseConfig(ConfigFile2)
// 		must(err)
// 	} else {
// 		must(err)
// 	}
// 	l, err := c.GetLogger(c.Log)
// 	must(err)
// 	logger = l
// 	return c
// }

func run() {
	var err error
	must(err)
	c, err := config.ParseConfig(ConfigFile)
	if os.IsNotExist(err) {
		must(err)
	} else {
		must(err)
	}
	if c.PidFile != "" {
		pid, err := pidfile.New(c.PidFile)
		if err != nil {
			must(fmt.Errorf("open pid file failed: %s", err))
		}
		defer pid.Remove()
	}

	tcpListeners, websockets, err := GetListeners(c)
	must(err)
	l, err := c.GetLogger(c.Log)
	must(err)
	logger = l

	s := server.New(
		server.WithConfig(c),
		server.WithTCPListener(tcpListeners...),
		server.WithWebsocketServer(websockets...),
		server.WithLogger(l),
	)

	err = s.Init()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
		return
	}
	// go installSignal(s)
	err = s.Run()
	if err != nil {
		fmt.Fprint(os.Stderr, err.Error())
		os.Exit(1)
		return
	}
}
