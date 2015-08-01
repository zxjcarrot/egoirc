package egoirc

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

const (
	defaultReadCmdBuf   = 512
	defaultWriteCmdBuf  = 512
	defaultPingInterval = 10 * time.Second
	defaultRegInteval   = 2 * time.Second
	defaultRegTries     = 10
)

// Setup represents configuration for a particular irc client
type Setup struct {
	// maximum # of buffered commands in the read channel, @defaultReadCmdBuf is used if 0 provided
	readBufCnt int
	// maximum # of buffered commands in the write channel, @defaultWriteCmdBuf is used if 0 provided
	writeBufCnt int
	// takes the form of "host[:port]" where host could be ip address or hostname and
	// default port 6667 is used if omitted.
	address string
	// if to use tls protocol
	tls bool
	// @interval the interval in second to send the ping command to the server
	// @defaultPingInterval is used if @interval 0 provided
	pingInterval time.Duration
	// nickname used on the irc network
	nickname string
	// realname used on the irc network, nickname is used if not provided
	realname string
	// hostname representing this client on the irc network
	hostname string
	// @regInterval the interval in second to register to the server
	// @defaultRegInteval is used if @interval 0 provided
	regInterval time.Duration
	// # of registeration tries w/o success to be considered failed
	regTries int
}

// Client acts as a client to an irc server
// Goroutines created for every Client:
//    main goroutine : the goroutine that calls Pump(), acts as a multiplexer for various events(io, timer etc)
//                     the user-provided callbacks will be called on this goroutine
//    timer goroutine: sends ping command every @interval to server to to keep the connection alive
//    reader goroutine : blocks at the io.Reader.Read() of the connection, parses the data into lines,
//                     delivers commands to main goroutine one line at a time
//    writer goroutine : waits for commands from the client, turn the commands into lines,
//                       delivers lines to server
type Client struct {
	conn       ircConn
	setup      Setup
	cbs        map[Event]*eventData
	readc      chan *Command
	writec     chan *Command
	errc       chan error
	eregc      chan *eventData // for event registeration
	eunregc    chan Event      // for event unregistration
	pingTicker *time.Ticker
	regTicker  *time.Ticker
	// # of times if this client tried to registered to the server.
	// a client is successfully registered when server sends back RPL_WELCOME reply, and this field is marked -1.
	// Client exits when the this field exceeded @setup.regTries
	regTried int
	stopped  bool
	mu       sync.Mutex // for stopped
}

func cbDefault(e Event, c *Command, err error, data interface{}) bool {
	//fmt.Printf("event %s, command %v, err %v, data %p\n", string(e), c, err, data)
	return true
}

func NewClient(s Setup) (*Client, error) {
	var cli Client
	if s.pingInterval == 0 {
		s.pingInterval = defaultPingInterval
	}
	if s.regInterval == 0 {
		s.regInterval = defaultRegInteval
	}
	if s.regTries == 0 {
		s.regTries = defaultRegTries
	}
	if !strings.Contains(s.address, ":") {
		s.address += ":" + defaultPort
	}
	if s.nickname == "" {
		return nil, errors.New("nickname is required")
	}
	if s.realname == "" {
		s.realname = s.nickname
	}
	if s.readBufCnt == 0 {
		s.readBufCnt = defaultReadCmdBuf
	}
	if s.writeBufCnt == 0 {
		s.writeBufCnt = defaultWriteCmdBuf
	}

	cli.setup = s
	cli.cbs = make(map[Event]*eventData)

	// default event data
	ed := eventData{EV_DEFAULT, cbDefault, &cli}
	cli.cbs[EV_DEFAULT] = &ed
	cli.cbs[EV_DISCONNECTED] = &ed
	cli.cbs[EV_CONNECTED] = &ed
	cli.cbs[EV_NET_ERROR] = &ed
	cli.cbs[EV_ERROR] = &ed

	return &cli, nil
}

func (cli *Client) Connect() (err error) {
	cli.conn, err = newConn(&cli.setup)
	if err != nil {
		return
	}
	cli.pingTicker = time.NewTicker(cli.setup.pingInterval)
	cli.regTicker = time.NewTicker(cli.setup.regInterval)
	cli.readc = make(chan *Command, cli.setup.readBufCnt)
	cli.writec = make(chan *Command, cli.setup.writeBufCnt)
	cli.errc = make(chan error, 10)
	cli.eregc = make(chan *eventData, 10)
	cli.eunregc = make(chan Event, 10)
	cli.cbs[EV_CONNECTED].handler(EV_CONNECTED, nil, nil, cli.cbs[EV_CONNECTED].data)
	return
}

// Register installs the given handler with @data on event @e
// returns true if the installation is successfully, false otherwise
func (cli *Client) Register(e Event, handler EventHandler, data interface{}) {
	var ed eventData
	ed.e = e
	ed.handler = handler
	ed.data = data
	if cli.eregc == nil { // before connecting to the server, register handlers directly
		cli.cbs[e] = &ed
	} else {
		cli.eregc <- &ed
	}
}

// Register removes the given handler on event @e
// returns true if the removal is successfully, false otherwise
func (cli *Client) Unregister(e Event) {
	if cli.eunregc == nil { // before connecting to the server, unregister handlers directly
		delete(cli.cbs, e)
	} else {
		cli.eunregc <- e
	}
}

func (cli *Client) doRead() {
	// recover from writing to closed channel
	defer func() {
		if x := recover(); x != nil {
			fmt.Println("doRead panic: ", x)
		}
	}()
	for {
		// keep reading until the connection is closed
		line, err := cli.conn.readLine()
		if err != nil {
			cli.errc <- err
			return
		}
		var cmd Command
		err = cmd.unmarshal(line)
		if err != nil {
			cli.errc <- err
		} else {
			cli.readc <- &cmd
		}
	}
}

func (cli *Client) doWrite() {
	defer func() {
		if x := recover(); x != nil {
			fmt.Println("doWrite panic: ", x)
		}
	}()
	for {
		cmd, ok := <-cli.writec
		if !ok { // main goroutine tells to close the
			return
		}
		line, err := cmd.marshal()
		//fmt.Printf("outgoing line:[%s]\n", line)
		if err != nil {
			cli.errc <- err
		} else {
			err := cli.conn.writeLine(line)
			if err != nil {
				cli.errc <- err
				return
			}
		}
	}
}

func (cli *Client) ping() {
	cli.SendCommand("", "PING", cli.setup.address)
}

func (cli *Client) nick() {
	cli.SendCommand("", "NICK", cli.setup.nickname)
}

func (cli *Client) user() {
	host := "*"
	if cli.setup.hostname != "" {
		host = cli.setup.hostname
	}
	cli.SendCommand("", "USER", cli.setup.nickname, "8", host, ":"+cli.setup.realname)
}

// SendMesage sends a message to a receiver
// Since irc protocol limits the length of a packet to 512 bytes,
// the message will be properly fragmented into several packets if too long.
// @receiver takes three form:
//                   a single channel: #channel1
//    				 a single nickname: paul
//                   a group of nicknames or channels seprated by comma:  paul,#channel1,mike,#channel2
// @msg the message to send
func (cli *Client) SendMessage(receiver, msg string) {
	min := 512 - (len(receiver) + 7 + 2) // 7 for "PRIVMSG", 2 for two spaces
	for i := 0; i < len(msg); i += min {
		start := i
		end := start + min
		if end > len(msg) {
			end = len(msg)
		}
		cli.SendCommand("", "PRIVMSG", receiver, msg[start:end])
	}
}

// SendCommand sends a command to the irc server
// see https://tools.ietf.org/html/rfc2812#section-2.3 for detailed definition of command
//
func (cli *Client) SendCommand(prefix, name string, params ...string) {
	cli.writec <- newCommand(prefix, name, params...)
}

func (cli *Client) multiplexError(e error) bool {
	var ed *eventData
	if err, ok := e.(Error); ok {
		if ed, ok = cli.cbs[Event(err2event[err.code])]; !ok {
			ed = cli.cbs[EV_DEFAULT]
		}
		return ed.handler(Event(err2event[err.code]), nil, err, ed.data)
	} else if err, ok := e.(net.Error); ok {
		ed = cli.cbs[EV_NET_ERROR]
		ed.handler(EV_NET_ERROR, nil, err, ed.data)
		return false
	} else {
		ed = cli.cbs[EV_ERROR]
		ed.handler(EV_ERROR, nil, e, ed.data)
		return false
	}
}

func (cli *Client) multiplexCmd(c *Command) bool {
	var (
		ed *eventData
		ok = false
	)
	switch c.name {
	case "001": //RPL_WELCOME
		// marked as registered
		cli.regTried = -1
		// stop registering ticker
		cli.regTicker.Stop()
	case "004":
		// save server's real hostname since the server user provided may behind a DNS load balancer
		cli.setup.address = c.params[0]
	}

	if ed, ok = cli.cbs[Event(c.name)]; !ok {
		ed = cli.cbs[EV_DEFAULT]
	}
	return ed.handler(Event(c.name), c, nil, ed.data)
}

// Spin registers to the irc server using nickname, realname, hostname provided by setup configuration
// and starts event multiplexing
// Spin returns when Client.Stop() is called or
func (cli *Client) Spin() {
	cli.nick()
	cli.user()
	go cli.doRead()
	go cli.doWrite()

loop:
	for {
		select {
		case _, ok := <-cli.pingTicker.C:
			if !ok {
				break loop
			}
			//fmt.Println("ping timer triggereds")
			cli.ping()
		case <-cli.regTicker.C:
			if cli.regTried < 0 {
				// registered
				continue
			} else if cli.regTried < cli.setup.regTries {
				//retrying
				//fmt.Println("retry registering", cli.regTried, "times.")
				cli.nick()
				cli.user()
				cli.regTried++
			} else if cli.regTried >= cli.setup.regTries {
				//failed
				//fmt.Println("failed to register to server", cli.setup.address)
				break loop
			}
		case e, ok := <-cli.errc:
			//fmt.Println("incoming error:", e)
			if !ok || !cli.multiplexError(e) {
				break loop
			}
		case cmd, ok := <-cli.readc:
			//fmt.Println("incoming command:", cmd)
			if !ok || !cli.multiplexCmd(cmd) {
				break loop
			}
		case ed, ok := <-cli.eregc:
			if !ok {
				break loop
			}
			cli.cbs[ed.e] = ed
			//fmt.Println("registered handler:", ed)
		case e, ok := <-cli.eunregc:
			if !ok {
				break loop
			}
			delete(cli.cbs, e)
			//fmt.Println("unregistered event:", e)
		}
	}
	fmt.Println("out")
	cli.Stop()
}

func (cli *Client) Stop() {
	cli.mu.Lock()
	defer cli.mu.Unlock()
	if cli.stopped {
		return
	}

	cli.conn.Close()
	cli.cbs[EV_DISCONNECTED].handler(EV_DISCONNECTED, nil, nil, cli.cbs[EV_DISCONNECTED].data)
	cli.pingTicker.Stop()
	close(cli.readc)
	close(cli.writec)
	close(cli.eregc)
	close(cli.eunregc)
	cli.stopped = true
}
