package klorq

import (
	"context"
	"errors"
	"io"
	"net"
	"time"

	gatewayv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/gateway/v1"
)

// OpenSSHForward opens an OpenShell ForwardTcp stream to the sandbox's SSH
// relay and exposes the full-duplex stream as a local TCP connection.
func (c *Client) OpenSSHForward(ctx context.Context, sandbox, sessionToken string) (net.Conn, error) {
	if ctx == nil {
		return nil, errors.New("SSH forward context is required")
	}
	if sandbox == "" || sessionToken == "" {
		return nil, errors.New("SSH forward sandbox and session token are required")
	}
	client, err := c.gatewayClient()
	if err != nil {
		return nil, err
	}
	streamCtx, cancel := context.WithCancel(ctx)
	stream, err := client.ForwardTcp(streamCtx)
	if err != nil {
		cancel()
		return nil, err
	}
	if err := stream.Send(&gatewayv1.TcpForwardFrame{Payload: &gatewayv1.TcpForwardFrame_Init{Init: &gatewayv1.TcpForwardInit{
		Sandbox: sandbox, AuthorizationToken: sessionToken,
		Target: &gatewayv1.TcpForwardInit_Ssh{Ssh: &gatewayv1.SshRelayTarget{}},
	}}}); err != nil {
		cancel()
		return nil, err
	}
	clientConn, bridgeConn, err := localTCPPair()
	if err != nil {
		cancel()
		return nil, err
	}
	go bridgeForwardTCP(streamCtx, cancel, stream, bridgeConn)
	return clientConn, nil
}

// OpenTCPForward opens an OpenShell ForwardTcp stream to a sandbox loopback
// port and exposes it as a local full-duplex connection.
func (c *Client) OpenTCPForward(ctx context.Context, sandbox, serviceID, host string, port uint32) (net.Conn, error) {
	if ctx == nil {
		return nil, errors.New("TCP forward context is required")
	}
	if sandbox == "" || port == 0 || port > 65535 {
		return nil, errors.New("TCP forward requires a sandbox and a valid target port")
	}
	if host == "" {
		host = "127.0.0.1"
	}
	client, err := c.gatewayClient()
	if err != nil {
		return nil, err
	}
	streamCtx, cancel := context.WithCancel(ctx)
	stream, err := client.ForwardTcp(streamCtx)
	if err != nil {
		cancel()
		return nil, err
	}
	if err := stream.Send(&gatewayv1.TcpForwardFrame{Payload: &gatewayv1.TcpForwardFrame_Init{Init: &gatewayv1.TcpForwardInit{
		Sandbox: sandbox, Workspace: c.workspace(), ServiceId: serviceID,
		Target: &gatewayv1.TcpForwardInit_Tcp{Tcp: &gatewayv1.TcpRelayTarget{Host: host, Port: port}},
	}}}); err != nil {
		cancel()
		return nil, err
	}
	clientConn, bridgeConn, err := localTCPPair()
	if err != nil {
		cancel()
		return nil, err
	}
	go bridgeForwardTCP(streamCtx, cancel, stream, bridgeConn)
	return clientConn, nil
}

func localTCPPair() (net.Conn, net.Conn, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, nil, err
	}
	defer listener.Close()
	client, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		return nil, nil, err
	}
	bridge, err := listener.Accept()
	if err != nil {
		_ = client.Close()
		return nil, nil, err
	}
	if tcp, ok := client.(*net.TCPConn); ok {
		_ = tcp.SetNoDelay(true)
	}
	if tcp, ok := bridge.(*net.TCPConn); ok {
		_ = tcp.SetNoDelay(true)
	}
	return client, bridge, nil
}

func bridgeForwardTCP(ctx context.Context, cancel context.CancelFunc, stream gatewayv1.OpenShell_ForwardTcpClient, conn net.Conn) {
	defer cancel()
	defer conn.Close()
	sendDone := make(chan error, 1)
	recvDone := make(chan error, 1)
	go func() {
		buffer := make([]byte, 32*1024)
		for {
			n, err := conn.Read(buffer)
			if n > 0 {
				if sendErr := stream.Send(&gatewayv1.TcpForwardFrame{Payload: &gatewayv1.TcpForwardFrame_Data{Data: append([]byte(nil), buffer[:n]...)}}); sendErr != nil {
					sendDone <- sendErr
					return
				}
			}
			if err != nil {
				if errors.Is(err, io.EOF) {
					sendDone <- stream.CloseSend()
				} else {
					sendDone <- err
				}
				return
			}
		}
	}()
	go func() {
		for {
			frame, err := stream.Recv()
			if err != nil {
				if errors.Is(err, io.EOF) {
					if half, ok := conn.(interface{ CloseWrite() error }); ok {
						recvDone <- half.CloseWrite()
						return
					}
					recvDone <- nil
					return
				}
				recvDone <- err
				return
			}
			data := frame.GetData()
			for len(data) > 0 {
				n, writeErr := conn.Write(data)
				if writeErr != nil {
					recvDone <- writeErr
					return
				}
				data = data[n:]
			}
		}
	}()
	sendFinished, recvFinished := false, false
	for !sendFinished || !recvFinished {
		select {
		case err := <-sendDone:
			sendFinished = true
			if err != nil {
				return
			}
		case err := <-recvDone:
			recvFinished = true
			if err != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}
