package main

import (
	"flag"
	"fmt"

	"cscan/rpc/task/internal/config"
	"cscan/rpc/task/internal/server"
	"cscan/rpc/task/internal/svc"
	"cscan/rpc/task/pb"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var configFile = flag.String("f", "etc/task.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c)
	logx.MustSetup(c.Log)
	logx.DisableStat()
	fmt.Println(`
   ______ _____  ______          _   _ 
  / ____/ ____|/ __ \ \        / / | \ | |
 | |   | (___ | |  | \ \  /\  / /|  \| |
 | |    \___ \| |  | |\ \/  \/ / | .  |
 | |________) | |__| | \  /\  /  | |\  |
  \_____|_____/ \____/   \/  \/   |_| \_| 
                  RPC SERVICE            `)
	fmt.Println("---------------------------------------------------------")
	logx.Infof("🚀 Initializing CScan Task RPC Service...")
	fmt.Println("---------------------------------------------------------")
	ctx := svc.NewServiceContext(c)

	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		pb.RegisterTaskServiceServer(grpcServer, server.NewTaskServiceServer(ctx))

		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})

	s.AddOptions(
		grpc.MaxRecvMsgSize(50*1024*1024), // 50MB
		grpc.MaxSendMsgSize(50*1024*1024), // 50MB
	)
	defer s.Stop()

	fmt.Println("---------------------------------------------------------")
	logx.Infof("✅ RPC Server listening at %s", c.ListenOn)
	logx.Infof("📡 Ready to handle gRPC requests...")
	fmt.Println("---------------------------------------------------------")
	s.Start()
}
