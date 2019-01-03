package core

/*
#cgo CFLAGS: -I./src/include
#include "lwip/tcp.h"
#include "lwip/udp.h"
#include "lwip/timeouts.h"
*/
import "C"
import (
	"sync"
	"time"
	"unsafe"
)

const CHECK_TIMEOUTS_INTERVAL = 250                                     // in millisecond
const TCP_POLL_INTERVAL = 2 * (float32(1000) / CHECK_TIMEOUTS_INTERVAL) // poll every 2 seconds

type LWIPStack interface {
	Write([]byte) (int, error)
	Close() error
	RestartTimeouts()
}

// lwIP runs in a single thread, locking is needed in Go runtime.
var lwipMutex = &sync.Mutex{}

type lwipStack struct {
	tpcb *C.struct_tcp_pcb
	upcb *C.struct_udp_pcb
}

func NewLWIPStack() LWIPStack {
	tcpPCB := C.tcp_new()
	if tcpPCB == nil {
		panic("tcp_new return nil")
	}

	err := C.tcp_bind(tcpPCB, C.IP_ADDR_ANY, 0)
	switch err {
	case C.ERR_OK:
		break
	case C.ERR_VAL:
		panic("invalid PCB state")
	case C.ERR_USE:
		panic("port in use")
	default:
		C.memp_free(C.MEMP_TCP_PCB, unsafe.Pointer(tcpPCB))
		panic("unknown tcp_bind return value")
	}

	tcpPCB = C.tcp_listen_with_backlog(tcpPCB, C.TCP_DEFAULT_LISTEN_BACKLOG)
	if tcpPCB == nil {
		panic("can not allocate tcp pcb")
	}

	// We can't call C function with Go functions as arguments here, it will
	// fail in compile time:
	// cannot use TCPAcceptFn (type func(unsafe.Pointer, *_Ctype_struct_tcp_pcb, _Ctype_schar) _Ctype_schar) as type *[0]byte in argument to func literal
	// I can't find other workarounds.
	// C.tcp_accept(tcpPCB, TCPAcceptFn)
	SetTCPAcceptCallback(tcpPCB)

	udpPCB := C.udp_new()
	if udpPCB == nil {
		panic("could not allocate udp pcb")
	}

	err = C.udp_bind(udpPCB, C.IP_ADDR_ANY, 0)
	if err != C.ERR_OK {
		panic("address already in use")
	}

	SetUDPRecvCallback(udpPCB, nil)

	go func() {
		for {
			select {
			case <-time.After(CHECK_TIMEOUTS_INTERVAL * time.Millisecond):
				lwipMutex.Lock()
				C.sys_check_timeouts()
				lwipMutex.Unlock()
			}
		}
	}()

	return &lwipStack{
		tpcb: tcpPCB,
		upcb: udpPCB,
	}
}

func (s *lwipStack) Write(data []byte) (int, error) {
	return Input(data)
}

func (s *lwipStack) RestartTimeouts() {
	C.sys_restart_timeouts()
}

func (s *lwipStack) Close() error {
	tcpConns.Range(func(_, c interface{}) bool {
		c.(*tcpConn).Abort()
		return true
	})
	udpConns.Range(func(_, c interface{}) bool {
		c.(*udpConn).Close()
		return true
	})
	return nil
}

func init() {
	lwipInit()
}
