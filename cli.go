package egoirc

import (
	"errors"
	"log"
	"net"
	"strconv"
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

// bit-or flags for usermode,
//see https://tools.ietf.org/html/rfc2812#section-3.1.5 for detail about user mode
const (
	UMODE_AWAY           = 1                         // user is flagged as away;
	UMODE_INVISIBLE      = UMODE_AWAY << 1           // marks a users as invisible;
	UMODE_WALLOPS        = UMODE_INVISIBLE << 1      // user receives wallops;
	UMODE_RESTRICTED     = UMODE_WALLOPS << 1        // restricted user connection;
	UMODE_OPERATOR       = UMODE_RESTRICTED << 1     // operator flag;
	UMODE_LOCAL_OPERATOR = UMODE_OPERATOR << 1       // locale operator flag
	UMODE_SERVER_NOTICE  = UMODE_LOCAL_OPERATOR << 1 // marks a user for receipt of server notices.
)

var modeStrings = [...]string{ //user mode character
	UMODE_AWAY:           "a",
	UMODE_INVISIBLE:      "i",
	UMODE_WALLOPS:        "w",
	UMODE_RESTRICTED:     "r",
	UMODE_OPERATOR:       "o",
	UMODE_LOCAL_OPERATOR: "O",
	UMODE_SERVER_NOTICE:  "s",
}

func flagToModeString(flags int) (ret string) {
	var i uint
	for i = 0; (1 << i) <= UMODE_SERVER_NOTICE; i++ {
		if flags&(1<<i) != 0 {
			ret += modeStrings[i]
		}
	}
	return
}

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
	//tls bool
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
	//log.Printf("event %s, command %v, err %v, data %p\n", string(e), c, err, data)
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

func (cli *Client) readCommand() {
	// recover from writing to closed channel
	defer func() {
		if x := recover(); x != nil {
			log.Println("readCommand panic: ", x)
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

func (cli *Client) writeCommand() {
	defer func() {
		if x := recover(); x != nil {
			log.Println("writeCommand panic: ", x)
		}
	}()
	for {
		cmd, ok := <-cli.writec
		if !ok { // main goroutine tells to close the
			return
		}
		line, err := cmd.marshal()
		//log.Printf("outgoing line:[%s]\n", line)
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

// Ping issues a <ping> command to server indicated by @address
func (cli *Client) Ping(address string) {
	cli.SendCommand("", "PING", address)
}

// Nick issues a <nick> command to server
func (cli *Client) Nick(nickname string) {
	cli.SendCommand("", "NICK", nickname)
}

// User issues a <user> command to irc server
// @nickname the nickname used in <nick> command
// @mode bit-or flags of initial user mode, only UMODE_INVISIBLE and UMODE_WALLOPS are available for initial user mode.
// 		 e.g. UMODE_INVISIBLE | UMODE_WALLOPS
// @host hostname of this client, "*" is used if empty string provided
// @realname realname for this client
func (cli *Client) User(nickname string, mode int, host, realname string) {
	// bit 2 for UMODE_WALLOPS, bit 3 for UMODE according to rfc2821
	realmode := 0
	if mode&UMODE_WALLOPS != 0 {
		realmode |= 1 << 2
	}
	if mode&UMODE_INVISIBLE != 0 {
		realmode |= 1 << 3
	}
	if host == "" {
		host = "*"
	}

	cli.SendCommand("", "USER", nickname, strconv.Itoa(realmode), host, ":"+realname)
}

// PrivMsg sends a message to a receiver
// Since irc protocol limits the length of a packet to 512 bytes,
// the message will be properly fragmented into several packets if too long.
// @receiver takes three form:
//                   a single channel: #channel1
//    				 a single nickname: paul
//                   a group of nicknames or channels seprated by comma:  paul,#channel1,mike,#channel2
// @msg the message to send
func (cli *Client) PrivMsg(receiver, msg string) {
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

// Mode issues a <mode> command to the irc server
// @nickname the nickname of this client. For a user to successfully issue this command ,
//           this must be the same one when registered to the server.
// @onFlags the bit-or flags of modes to be turned on on this client.
// @unsetFlags the bit-or flags of modes to be turned off on this client
func (cli *Client) Mode(nickname string, onFlags, offFlags int) {
	onString := flagToModeString(onFlags)
	offString := flagToModeString(offFlags)
	params := make([]string, 2)
	if onString != "" {
		params = append(params, "+"+onString)
	}
	if offString != "" {
		params = append(params, "-"+offString)
	}
	if len(params) == 0 {
		return
	}
	cli.SendCommand("", "MODE", params...)
}

// SendCommand sends a command to the irc server
// see https://tools.ietf.org/html/rfc2812#section-2.3 for detailed definition of commands
func (cli *Client) SendCommand(prefix, name string, params ...string) {
	var cmd Command
	cmd.prefix = prefix
	cmd.name = name
	cmd.params = append(cmd.params, params...)
	cli.writec <- &cmd
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
	cli.Nick(cli.setup.nickname)
	cli.User(cli.setup.nickname, UMODE_INVISIBLE, "", cli.setup.realname)
	go cli.readCommand()
	go cli.writeCommand()

loop:
	for {
		select {
		case _, ok := <-cli.pingTicker.C:
			if !ok {
				break loop
			}
			//log.Println("ping timer triggereds")
			cli.Ping(cli.setup.address)
		case <-cli.regTicker.C:
			if cli.regTried < 0 {
				// registered
				continue
			} else if cli.regTried < cli.setup.regTries {
				//retrying
				//log.Println("retry registering", cli.regTried, "times.")
				cli.Nick(cli.setup.nickname)
				cli.User(cli.setup.nickname, UMODE_INVISIBLE, "", cli.setup.realname)
				cli.regTried++
			} else if cli.regTried >= cli.setup.regTries {
				//failed
				//log.Println("failed to register to server", cli.setup.address)
				break loop
			}
		case e, ok := <-cli.errc:
			//log.Println("incoming error:", e)
			if !ok || !cli.multiplexError(e) {
				break loop
			}
		case cmd, ok := <-cli.readc:
			//log.Println("incoming command:", cmd)
			if !ok || !cli.multiplexCmd(cmd) {
				break loop
			}
		case ed, ok := <-cli.eregc:
			if !ok {
				break loop
			}
			cli.cbs[ed.e] = ed
			//log.Println("registered handler:", ed)
		case e, ok := <-cli.eunregc:
			if !ok {
				break loop
			}
			delete(cli.cbs, e)
			//log.Println("unregistered event:", e)
		}
	}
	log.Println("out")
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
