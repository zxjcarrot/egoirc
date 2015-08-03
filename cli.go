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
	defaultEventBuf     = 1024
	defaultWriteCmdBuf  = 512
	defaultPingInterval = 10 * time.Second
	defaultRegInteval   = 2 * time.Second
	defaultRegTries     = 10
)

func init() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
}

// bits or'ed flags for usermode,
//see https://tools.ietf.org/html/rfc2812#section-3.1.5 for detail about user mode
const (
	UMODE_AWAY           = 1 << iota // user is flagged as away;
	UMODE_INVISIBLE                  // marks a users as invisible;
	UMODE_WALLOPS                    // user receives wallops;
	UMODE_RESTRICTED                 // restricted user connection;
	UMODE_OPERATOR                   // operator flag;
	UMODE_LOCAL_OPERATOR             // locale operator flag
	UMODE_SERVER_NOTICE              // marks a user for receipt of server notices.
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

type empty struct{}

// Setup represents configuration for a particular irc client
type Setup struct {
	// maximum # of buffered event in the event channel, @defaultEventBuf is used if 0 provided
	EventBufCnt int
	// maximum # of buffered commands in the write channel, @defaultWriteCmdBuf is used if 0 provided
	WriteBufCnt int
	// takes the form of "host[:port]" where host could be ip address or hostname and
	// default port 6667 is used if omitted.
	Address string
	// if to use tls protocol
	//tls bool
	// @interval the interval in second to send the ping command to the server
	// @defaultPingInterval is used if @interval 0 provided
	PingInterval time.Duration
	// nickname used on the irc network
	Nickname string
	// realname used on the irc network, nickname is used if not provided
	Realname string
	// hostname representing this client on the irc network
	Hostname string
	// @RegInterval the interval in second to register to the server
	// @defaultRegInteval is used if @interval 0 provided
	RegInterval time.Duration
	// # of registeration tries w/o success to be considered failed
	RegTries int
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
	cbs        map[Event]*eventRegistry
	writec     chan *Command       // channel to deliver command to network i/o goroutine
	evc        chan *eventData     // channel to deliver event
	eregc      chan *eventRegistry //channel to deliver event handler registration
	eunregc    chan Event          // channel to deliver event hanlder unregistration
	closing    chan empty          // purely for cleanup signal
	pingTicker *time.Ticker
	regTicker  *time.Ticker
	// # of times if this client tried to registered to the server.
	// a client is successfully registered when server sends back RPL_WELCOME reply, and this field is marked -1.
	// Client exits when the this field exceeded @setup.RegTries
	regTried int
	stopped  bool
	mu       sync.Mutex
}

func cbDefault(e Event, c *Command, err error, data interface{}) bool {
	log.Printf("event %s, command %v, err %v, data %p\n", string(e), c, err, data)
	return true
}

func NewClient(s Setup) (*Client, error) {
	var cli Client
	if s.PingInterval == 0 {
		s.PingInterval = defaultPingInterval
	}
	if s.RegInterval == 0 {
		s.RegInterval = defaultRegInteval
	}
	if s.RegTries == 0 {
		s.RegTries = defaultRegTries
	}
	if !strings.Contains(s.Address, ":") {
		s.Address += ":" + defaultPort
	}
	if s.Nickname == "" {
		return nil, errors.New("nickname is required")
	}
	if s.Realname == "" {
		s.Realname = s.Nickname
	}
	if s.EventBufCnt == 0 {
		s.EventBufCnt = defaultEventBuf
	}
	if s.WriteBufCnt == 0 {
		s.WriteBufCnt = defaultWriteCmdBuf
	}

	cli.setup = s
	cli.cbs = make(map[Event]*eventRegistry)

	// default event data
	ed := eventRegistry{EV_DEFAULT, cbDefault, &cli}
	cli.cbs[EV_DEFAULT] = &ed

	return &cli, nil
}

func (cli *Client) getEventRegistry(e Event) *eventRegistry {
	var (
		er *eventRegistry
		ok bool
	)
	if er, ok = cli.cbs[e]; !ok {
		er = cli.cbs[EV_DEFAULT]
	}
	return er
}

func (cli *Client) Connect() (err error) {
	cli.conn, err = newConn(&cli.setup)
	if err != nil {
		return
	}
	cli.pingTicker = time.NewTicker(cli.setup.PingInterval)
	cli.regTicker = time.NewTicker(cli.setup.RegInterval)
	cli.writec = make(chan *Command, cli.setup.WriteBufCnt)
	cli.evc = make(chan *eventData, cli.setup.EventBufCnt)
	cli.eregc = make(chan *eventRegistry, 10)
	cli.eunregc = make(chan Event, 10)
	cli.closing = make(chan empty)
	er := cli.getEventRegistry(Event(EV_CONNECTED))
	er.handler(Event(EV_CONNECTED), nil, nil, er.data)
	return
}

func newEventData(et eventType, e Event, c *Command, err error) *eventData {
	return &eventData{et, e, c, err}
}

func newErrorEventData(err error) *eventData {
	return newEventData(et_error, "", nil, err)
}

func newCmdEventData(c *Command) *eventData {
	return newEventData(et_cmd, "", c, nil)
}

func newUserEventData(e Event) *eventData {
	return newEventData(et_user, e, nil, nil)
}

// Register installs the given handler with @data on event @e
// returns true if the installation is successfully, false otherwise
func (cli *Client) Register(e Event, handler EventHandler, data interface{}) {
	cli.mu.Lock()
	defer cli.mu.Unlock()
	var er eventRegistry
	er.e = e
	er.handler = handler
	er.data = data
	if cli.eregc == nil { // before connecting to the server, register handlers directly
		cli.cbs[e] = &er
	} else {
		cli.eregc <- &er
	}
}

// Register removes the given handler on event @e
// returns true if the removal is successfully, false otherwise
func (cli *Client) Unregister(e Event) {
	cli.mu.Lock()
	defer cli.mu.Unlock()
	var er eventRegistry
	er.e = e
	if cli.eunregc == nil { // before connecting to the server, unregister handlers directly
		delete(cli.cbs, e)
	} else {
		cli.eunregc <- e
	}
}

func (cli *Client) readCommand() {
	for {
		// keep reading until the connection is closed
		line, err := cli.conn.readLine()
		if err != nil {
			cli.evc <- newErrorEventData(err)
			return
		}
		var cmd Command
		err = cmd.unmarshal(line)
		if err != nil {
			cli.evc <- newErrorEventData(err)
		} else {
			cli.evc <- newCmdEventData(&cmd)
		}
	}
}

func (cli *Client) writeCommand() {
	for {
		select {
		case cmd := <-cli.writec:
			line, err := cmd.marshal()

			//log.Printf("outgoing cmd %v -> line:[%s]\n", cmd, line)

			if err != nil {
				cli.evc <- newErrorEventData(err)
			} else {
				err := cli.conn.writeLine(line)
				if err != nil {
					cli.evc <- newErrorEventData(err)
				}
			}
		case _ = <-cli.closing:
			return
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
// @mode bits or'ed flags of initial user mode, only UMODE_INVISIBLE and UMODE_WALLOPS are available for initial user mode.
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
// @onFlags the bits or'ed flags of modes to be turned on on this client.
// @unsetFlags the bits or'ed flags of modes to be turned off on this client
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
	cmd.Prefix = prefix
	cmd.Name = name
	cmd.Params = append(cmd.Params, params...)
	cli.writec <- &cmd
}

func (cli *Client) multiplexError(e error) bool {
	var er *eventRegistry
	if err, ok := e.(Error); ok {
		er = cli.getEventRegistry(Event(err2event[err.code]))
		return er.handler(Event(err2event[err.code]), nil, err, er.data)
	} else if err, ok := e.(net.Error); ok {
		er = cli.getEventRegistry(Event(EV_NET_ERROR))
		er.handler(EV_NET_ERROR, nil, err, er.data)
		return false
	} else {
		er = cli.getEventRegistry(Event(EV_ERROR))
		er.handler(EV_ERROR, nil, e, er.data)
		return false
	}
}

func (cli *Client) multiplexCmd(c *Command) bool {
	switch c.Name {
	case "001": //RPL_WELCOME
		// marked as registered
		cli.regTried = -1
		// stop registering ticker
		cli.regTicker.Stop()
	case "004":
		// save server's real hostname since the server user provided may behind a DNS load balancer
		cli.setup.Address = c.Params[0]
	}

	er := cli.getEventRegistry(Event(c.Name))
	return er.handler(Event(c.Name), c, nil, er.data)
}

func (cli *Client) multiplex(ed *eventData) bool {

	switch ed.et {
	case et_error:
		if ed.err == nil {
			log.Println("error event but err is nil")
			return false
		}
		return cli.multiplexError(ed.err)
	case et_cmd:
		if ed.c == nil {
			log.Println("command event but c is nil")
			return false
		}
		return cli.multiplexCmd(ed.c)
	case et_user:
		er := cli.getEventRegistry(ed.e)
		return er.handler(ed.e, nil, nil, er.data)
	}
	return true
}

func (cli *Client) Post(e Event) {
	cli.evc <- newUserEventData(e)
}

// Spin registers to the irc server using nickname, realname, hostname provided by setup configuration
// and starts event multiplexing
// Spin returns when Client.Stop() is called or
func (cli *Client) Spin() {
	cli.Nick(cli.setup.Nickname)
	cli.User(cli.setup.Nickname, UMODE_INVISIBLE, "", cli.setup.Realname)
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
			cli.Ping(cli.setup.Address)
		case <-cli.regTicker.C:
			if cli.regTried < 0 {
				// registered
				continue
			} else if cli.regTried < cli.setup.RegTries {
				//retrying
				//log.Println("retry registering", cli.regTried, "times.")
				cli.Nick(cli.setup.Nickname)
				cli.User(cli.setup.Nickname, UMODE_INVISIBLE, "", cli.setup.Realname)
				cli.regTried++
			} else if cli.regTried >= cli.setup.RegTries {
				//failed
				//log.Println("failed to register to server", cli.setup.Address)
				break loop
			}
		case ed, ok := <-cli.evc:
			//log.Println("incoming event:", ed)
			if !ok || !cli.multiplex(ed) {
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
		case _ = <-cli.closing:
			break loop
		}
	}
	log.Println("out")
	//clean up
	cli.Stop()
}

func (cli *Client) Stop() {
	cli.mu.Lock()
	defer cli.mu.Unlock()
	if cli.stopped == true {
		return
	}
	cli.conn.Close()
	er := cli.getEventRegistry(Event(EV_DISCONNECTED))
	er.handler(EV_DISCONNECTED, nil, nil, er.data)
	close(cli.closing) // signal other goroutines to exit
	cli.pingTicker.Stop()
	cli.regTicker.Stop()
	cli.stopped = true
}
