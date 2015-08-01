package egoirc

import (
	"log"
	"math/rand"
	"runtime"
	"testing"
	"time"
)

var s = Setup{
	address:      "irc.freenode.net",
	nickname:     "paulo",
	realname:     "paulo321",
	pingInterval: 10 * time.Second,
	regInterval:  5 * time.Second,
	regTries:     10,
}

func TestClient(t *testing.T) {
	cli, err := NewClient(s)

	if err != nil {
		t.Fatal(err)
	}

	if err = cli.Connect(); err != nil {
		t.Fatal(err)
	}
	n := runtime.NumGoroutine()
	go cli.Spin()
	if n == runtime.NumGoroutine() {
		t.Fatalf("no goroutine created!!!")
	}
	time.Sleep(10 * time.Second)
	cli.Stop()
	time.Sleep(2 * time.Second) // let goroutines die
	log.Println("stopped spinning")
	if n != runtime.NumGoroutine() {
		t.Fatalf("%d goroutines still running!!!", runtime.NumGoroutine()-n)
	}
}

func TestClientConnection(t *testing.T) {
	cli, err := NewClient(s)

	if err != nil {
		t.Fatal(err)
	}
	connected := false
	disconnected := true
	handleCONNECTED := EventHandler(func(e Event, c *Command, err error, data interface{}) bool {
		connected = true
		disconnected = false
		return true
	})
	handleDISCONNECTED := EventHandler(func(e Event, c *Command, err error, data interface{}) bool {
		connected = false
		disconnected = true
		return true
	})

	cli.Register(EV_CONNECTED, handleCONNECTED, nil)
	cli.Register(EV_DISCONNECTED, handleDISCONNECTED, nil)
	if err = cli.Connect(); err != nil {
		t.Fatal(err)
	}
	if connected == false {
		t.Fatal("not connected")
	}
	go cli.Spin()
	time.Sleep(10 * time.Second)
	cli.Stop()
	if disconnected == false {
		t.Fatal("still connected")
	}
}

func generateNickname() (name string) {
	for i := 0; i < 8; i++ {
		name += string((rand.Int() % 26) + 'a')
	}
	return
}
func TestClientMessage(t *testing.T) {
	cli1name := generateNickname()
	cli2name := generateNickname()
	s.nickname = cli1name
	s.realname = s.nickname
	cli1, err := NewClient(s)
	if err != nil {
		t.Fatal(err)
	}

	s.nickname = cli2name
	s.realname = s.nickname
	cli2, err := NewClient(s)
	if err != nil {
		t.Fatal(err)
	}

	ready := make(chan bool)

	handlePRIVMSG := EventHandler(func(e Event, c *Command, err error, data interface{}) bool {
		if err != nil {
			t.Fatal(err)
			return false
		}
		me := data.(string)
		log.Printf("[%s] %s %s says: %s\n", me, c.prefix, c.params[0], c.params[1])
		ready <- true
		return true
	})

	// tell main goroutine that client is registered and ready to talk
	handleRPLWELCOME := EventHandler(func(e Event, c *Command, err error, data interface{}) bool {
		log.Println(data.(string), "connected")
		if err != nil {
			ready <- false
			return false
		}
		ready <- true
		return true
	})

	cli1.Register("PRIVMSG", handlePRIVMSG, cli1name)
	// RPL_WELCOME
	cli1.Register("001", handleRPLWELCOME, cli1name)

	cli2.Register("PRIVMSG", handlePRIVMSG, cli2name)
	// RPL_WELCOME
	cli2.Register("001", handleRPLWELCOME, cli2name)
	if err = cli1.Connect(); err != nil {
		t.Fatal(cli1name, "failed to connect", err)
	}
	if err = cli2.Connect(); err != nil {
		t.Fatal(cli2name, "failed to connect", err)
	}

	go cli1.Spin()
	go cli2.Spin()

	// wait for 2 clients to get ready
	for i := 0; i < 2; i++ {
		ok := <-ready
		if !ok {
			cli1.Stop()
			cli2.Stop()
			t.Fatal("failed to Spin 2 clients")
			return
		}
	}

	log.Println("----------------- both are ready to send private messages --------------")
	cli1.PrivMsg(cli2name, "hi there, this is "+cli1name)
	cli2.PrivMsg(cli1name, "hi there, this is "+cli2name)

	timeout := time.After(10 * time.Second)
	// wait for 2 clients both received message
	for i := 0; i < 2; i++ {
		select {
		case <-ready:
		case <-timeout:
			cli1.Stop()
			cli2.Stop()
			t.Fatal("took too long!!!")
			return
		}
	}

	log.Println("----------------- both are ready to join channel --------------")
	cli1.SendCommand("", "JOIN", "#obe")
	cli2.SendCommand("", "JOIN", "#obe")
	time.Sleep(5 * time.Second)
	log.Println("----------------- both are ready to send channel messages --------------")
	cli1.PrivMsg("#obe", "hi there, this is "+cli1name)
	cli2.PrivMsg("#obe", "hi there, this is "+cli2name)

	timeout = time.After(10 * time.Second)
	// wait for 2 clients both received message
	for i := 0; i < 2; i++ {
		select {
		case <-ready:
		case <-timeout:
			cli1.Stop()
			cli2.Stop()
			t.Fatal("took too long!!!")
			return
		}
	}
	cli1.Stop()
	cli2.Stop()
}
