package grpc_server

import (
	"crypto/tls"
	"net"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"log"
	"reflect"
)

type grpcServerRunner struct {
	listenAddress   string
	handler         interface{}
	serverRegistrar interface{}
	tlsConfig       *tls.Config
}

// NewGRPCServer returns an ifrit.Runner for your GRPC server process, given artifacts normally generated from a
// protobuf service definition by protoc.
//
// tlsConfig is optional.  If nil the server will run insecure.
//
// handler must be an implementation of the interface generated by protoc.
//
// serverRegistrar must be the "RegisterXXXServer" method generated by protoc.
//
// Type checking occurs at runtime.  Poorly typed `handler` or `serverRegistrar` parameters will cause a panic.
func NewGRPCServer(listenAddress string, tlsConfig *tls.Config, handler, serverRegistrar interface{}) grpcServerRunner {
	vServerRegistrar := reflect.ValueOf(serverRegistrar)
	vHandler := reflect.ValueOf(handler)

	registrarType := vServerRegistrar.Type()
	handlerType := vHandler.Type()

	// registrar type must be `func(*grpc.Server, X)`
	if registrarType.Kind() != reflect.Func {
		log.Panicf("NewGRPCServer: `serverRegistrar` should be %s but is %s",
			reflect.Func, registrarType.Kind())
	}
	if registrarType.NumIn() != 2 {
		log.Panicf("NewGRPCServer: `serverRegistrar` should have 2 parameters but it has %d parameters",
			registrarType.NumIn())
	}
	if registrarType.NumOut() != 0 {
		log.Panicf("NewGRPCServer: `serverRegistrar` should return no value but it returns %d values",
			registrarType.NumOut())
	}

	// registrar's first parameter type must match handler type.
	if reflect.TypeOf((*grpc.Server)(nil)) != registrarType.In(0) {
		log.Panicf("NewGRPCServer: type of `serverRegistrar`'s first parameter must be `*grpc.Server` but is %s",
			registrarType.In(0))
	}

	// registrar's second parameter type must be implemented by handler type.
	if !handlerType.Implements(registrarType.In(1)) {
		log.Panicf("NewGRPCServer: type of `serverRegistrar`'s second parameter %s is not implemented by `handler` type %s",
			registrarType.In(1), handlerType)
	}

	return grpcServerRunner{
		listenAddress:   listenAddress,
		handler:         handler,
		serverRegistrar: serverRegistrar,
		tlsConfig:       tlsConfig,
	}
}

func (s grpcServerRunner) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	vServerRegistrar := reflect.ValueOf(s.serverRegistrar)
	vHandler := reflect.ValueOf(s.handler)

	lis, err := net.Listen("tcp", s.listenAddress)
	if err != nil {
		return err
	}

	opts := []grpc.ServerOption{}
	if s.tlsConfig != nil {
		opts = append(opts, grpc.Creds(credentials.NewTLS(s.tlsConfig)))
	}
	server := grpc.NewServer(opts...)
	args := []reflect.Value{reflect.ValueOf(server), vHandler}
	vServerRegistrar.Call(args)

	errCh := make(chan error)
	go func() {
		errCh <- server.Serve(lis)
	}()

	close(ready)

	select {
	case /* sig := */ <-signals:
		break
	case err = <-errCh:
		break
	}

	server.GracefulStop()
	return err
}
