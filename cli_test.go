package egoirc

import (
	"log"
	"math/rand"
	"runtime"
	"testing"
	"time"
)

var s = Setup{
	Address:      "irc.freenode.net",
	Nickname:     "paulo",
	Realname:     "paulo321",
	PingInterval: 10 * time.Second,
	RegInterval:  5 * time.Second,
	RegTries:     10,
}

func TestClient(t *testing.T) {
	log.Println("----------------TestClient begins----------------")
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
	log.Println("----------------TestClient ends----------------")
}

func TestClientConnection(t *testing.T) {
	log.Println("----------------TestClientConnection begins----------------")

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
	log.Println("----------------TestClientConnection ends----------------")
}

func generateNickname() (name string) {
	for i := 0; i < 8; i++ {
		name += string((rand.Int() % 26) + 'a')
	}
	return
}

func TestClientMessage(t *testing.T) {
	log.Println("----------------TestClientMessage begins----------------")

	rand.Seed(time.Now().UnixNano())
	cli1name := generateNickname()
	cli2name := generateNickname()
	s.Nickname = cli1name
	s.Realname = s.Nickname
	cli1, err := NewClient(s)
	if err != nil {
		t.Fatal(err)
	}

	s.Nickname = cli2name
	s.Realname = s.Nickname
	cli2, err := NewClient(s)
	if err != nil {
		t.Fatal(err)
	}

	ready := make(chan bool)

	handlePRIVMSG := EventHandler(func(e Event, c *Command, err error, data interface{}) bool {
		if err != nil {
			log.Println(err)
			t.Fatal(err)
			return false
		}
		me := data.(string)
		log.Printf("[%s] %s %s says: %s\n", me, c.Prefix, c.Params[0], c.Params[1])
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

	timeout = time.After(20 * time.Second)
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
	log.Println("----------------TestClientMessage ends----------------")
}

func TestClientPost(t *testing.T) {
	log.Println("----------------TestClientPost begins----------------")
	rand.Seed(time.Now().UnixNano())
	s.Nickname = generateNickname()
	cli, err := NewClient(s)
	if err != nil {
		t.Fatal(err)
	}

	// tell main goroutine that client is registered and ready to talk
	handleRPLWELCOME := EventHandler(func(e Event, c *Command, err error, data interface{}) bool {
		log.Println(data.(string), "registered")
		if err != nil {
			t.Fatal(err)
			return false
		}
		// registered, go invisible
		cli.Post(Event("hide"))
		return true
	})

	// user-defined event
	handleHIDE := EventHandler(func(e Event, c *Command, err error, data interface{}) bool {
		log.Println(data.(string), "triggered", e, "event, now go invisible")
		cli.Mode(s.Nickname, UMODE_INVISIBLE, 0)
		return true
	})

	// RPL_WELCOME
	cli.Register("001", handleRPLWELCOME, s.Nickname)
	cli.Register("hide", handleHIDE, s.Nickname)

	if err = cli.Connect(); err != nil {
		t.Fatal(err)
	}

	go cli.Spin()

	time.Sleep(15 * time.Second)
	cli.Stop()
	log.Println("----------------TestClientPost ends----------------")
}

func TestClientRPLList(t *testing.T) {
	log.Println("----------------TestClientRPLList begins----------------")
	rand.Seed(time.Now().UnixNano())
	s.Nickname = generateNickname()
	cli, err := NewClient(s)
	if err != nil {
		t.Fatal(err)
	}

	// timing
	var start int64

	// tell main goroutine that client is registered and ready to talk
	handleRPLWELCOME := EventHandler(func(e Event, c *Command, err error, data interface{}) bool {
		log.Println(data.(string), "registered")
		if err != nil {
			t.Fatal(err)
			return false
		}
		log.Println("now is", time.Now(), "starts pulling channel list")
		start = time.Now().Unix()
		// grab whole list of channels
		cli.SendCommand("", "list")
		return true
	})

	var cnt int64 = 0
	// tell main goroutine that client is registered and ready to talk
	handleRPLLIST := EventHandler(func(e Event, c *Command, err error, data interface{}) bool {
		if err != nil {
			t.Fatal(err)
			return false
		}
		log.Println(c.Prefix, c.Name, c.Params)
		cnt = cnt + 1
		return true
	})

	done := make(chan bool)
	// tell main goroutine that client is registered and ready to talk
	handleRPLLISTEND := EventHandler(func(e Event, c *Command, err error, data interface{}) bool {
		if err != nil {
			t.Fatal(err)
			done <- false
			return false
		}
		done <- true
		return true
	})

	// RPL_WELCOME
	cli.Register("001", handleRPLWELCOME, s.Nickname)
	// RPL_LIST
	cli.Register("322", handleRPLLIST, s.Nickname)
	// RPL_LISTEND
	cli.Register("323", handleRPLLISTEND, s.Nickname)

	if err = cli.Connect(); err != nil {
		t.Fatal(err)
	}

	go cli.Spin()

	timeout := time.After(30 * time.Second)
	// wait for listing ends or half a minute
	select {
	case <-done:
		break
	case <-timeout:
		log.Println("took too long!!!")
		break
	}
	cli.Stop()
	// wait for other goroutines to die
	time.Sleep(2 * time.Second)
	log.Println("now is", time.Now(), "done pulling channel list")
	now := time.Now().Unix()
	log.Println("took", now-start, "seconds,", cnt/(now-start), "/ ops")
	log.Println("----------------TestClientRPLList ends----------------")
}
