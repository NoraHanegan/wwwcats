package main

import (
	"log"
	"strings"
)

type Lobby struct {
	name string

	// Client management
	clients    map[*Client]bool
	register   chan *Client
	unregister chan *Client

	// Broadcast
	bcast        chan []byte
	complexBcast chan *ComplexBcast

	currentGame *Game
}

func newLobby(name string) (lobby *Lobby) {
	lobby = &Lobby{
		name:    name,
		clients: make(map[*Client]bool),

		// We make channels with a small buffer, in case we need to
		// write to them from their own goroutine for convenience

		register:     make(chan *Client, 64),
		unregister:   make(chan *Client, 64),
		bcast:        make(chan []byte, 64),
		complexBcast: make(chan *ComplexBcast, 64),
	}
	lobby.currentGame = newGame(lobby)
	return
}

func (l *Lobby) run(lobbies map[string]*Lobby) {
	// Goroutine to deal with all the tasks of the lobby

	for {
		select {

		case client := <-l.register:
			l.clients[client] = true

			// Sync the join to the game object
			l.currentGame.addPlayer(client)

		case client := <-l.unregister:
			// Announce and sync
			// We need to do this before we close the channel
			l.currentGame.removePlayer(client)

			if _, ok := l.clients[client]; ok {
				delete(l.clients, client)
				close(client.send)
			}

			if len(l.clients) == 0 {
				// The lobby is finished
				delete(lobbies, l.name)
				return
			}

		case bytes := <-l.bcast:
			for client := range l.clients {
				select {
				case client.send <- bytes:
				default:
					l.unregister <- client
				}
			}

		case bcast := <-l.complexBcast:
			for client := range l.clients {
				_, ok := bcast.except[client]
				if ok {
					// This client is in the exception list
					continue
				}

				select {
				case client.send <- []byte(bcast.text):
				default:
					l.unregister <- client
				}
			}

		}
	}
}

func (c *Client) joinToLobby(lobby_name string, player_name string, lobbies map[string]*Lobby) {
	var lobby *Lobby

	if lobbies[lobby_name] == nil {
		// Create the lobby, and start its goroutine
		lobby = newLobby(lobby_name)
		lobbies[lobby_name] = lobby
		go lobby.run(lobbies)
	} else {
		lobby = lobbies[lobby_name]
	}

	// Avoid nickname collisions
	for connected_client := range lobbies[lobby_name].clients {
		if connected_client.name == player_name {
			select {
			case c.send <- []byte("err username_exists"):
			default:
				close(c.send)
			}
			// Nobody is getting joined to the lobby today
			return
		}
	}

	c.name = player_name

	lobby.register <- c
	c.lobby = lobby
}

func (l *Lobby) readFromClient(c *Client, msg string) {
	fields := strings.Fields(msg)

	// Lobby-wide commands

	if fields[0] == "chat" {
		l.sendBcast("chat " + c.name + " " + msg[5:])
		return
	}

	// Nothing to be done here, hand the message off to the game object
	l.currentGame.readFromClient(c, msg)
}

func (l *Lobby) sendBcast(msg string) {
	select {
	case l.bcast <- []byte(msg):
	default:
		log.Fatal("failed to broadcast message", msg)
	}
}

type ComplexBcast struct {
	// We use this type when we need to send a message to all clients _except_ one
	except map[*Client]bool
	text   string
}

func (l *Lobby) sendComplexBcast(text string, except map[*Client]bool) {
	bcast := &ComplexBcast{
		except: except,
		text:   text,
	}

	select {
	case l.complexBcast <- bcast:
	default:
		log.Fatal("failed to broadcast complex message", bcast.text)
	}
}
