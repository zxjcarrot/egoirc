##Overview
egoirc is a event-driven irc client library.

##Command
egoirc implements several easy-to-use wrappers for common irc commands, more complicated command could be implemented by sending raw text, see `Client.SendCommand` method.

##Event
`Event` is defined as string and take the following form:
- irc command: QUIT, PRIVMSG etc..., see https://en.wikipedia.org/wiki/List_of_Internet_Relay_Chat_commands for details.  
- irc reply number: 001, 311 etc..., see https://tools.ietf.org/html/rfc2812#section-5 for details.  
- connection status change events: see `event.go`.  
- error events: see `err.go` for detailed events.  
- user-provided string: any string(should avoid name collision with above events) that to be considered user-defined events.

##Handler
In egoirc, handler is called when interested events occured.  
All handlers are called from the same goroutine that called `Client.Spin()` method.

##User-defined events
egoirc also works with user-defined events, simply use `Client.Post` method to post a user-defined event to the library, and at later time the corresponding handler will be called. With this, one can easily implements handler chaining.

##Usage

```
/* set up configurations, check out Setup struct in cli.go */
var s egoirc.Setup
s.address = "irc.freenode.net"
s.nickname = "yournickname"
s.realname = "yourrealname" // nickname will be used if not provided
s.hostname = "yourhostname" // "*" will be used if not provided
s.pingInterval = 10 * time.Second // default to 10s if not provided

cli, err := egoirc.NewClient(s)
if err != nil {
	t.Fatal(err)
}

// called when there is message
handlePRIVMSG := egoirc.EventHandler(func(e Event, c *Command, err error, data interface{}) bool {
        //convert back to string
        me := data.(string)
		log.Printf("[%s] %s %s says: %s\n", me, c.prefix, c.params[0], c.params[1])
		return true
})

// called when registered
handleRPLWELCOME := egoirc.EventHandler(func(e Event, c *Command, err error, data interface{}) bool {
        //convert back to string
    	log.Println(data.(string), "connected")
    	//now registered, join a channel
    	cli.SendCommand("", "JOIN", "#go-nuts")
    	return true
})

// register handlers
cli.Register("PRIVMSG", handlePRIVMSG, "foo")
cli.Register("001", handleRPLWELCOME, "bar")

// connect to server
if err = cli.Connect(); err != nil {
	log.Fatal("failed to connect", err)
}

// Spin() blocks
go cli.Spin()

timeout = time.After(100 * time.Second)
// stop the other goroutine safely
cli.Stop()
```
##Examples
see `cli_test.go`.



