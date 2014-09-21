package main

import (
	"github.com/go-martini/martini"
	"github.com/gorilla/websocket"
	"github.com/martini-contrib/render"

	"encoding/json"
	"labix.org/v2/mgo"
	"labix.org/v2/mgo/bson"
	"log"
	"net"
	"net/http"
	_ "net/http/fcgi"
	"sync"
	"time"
)

var Rooms = make(map[string]map[ClientConn]int)
var ActiveClientRWMutex sync.RWMutex

const (
	GraphId           = "graph"
	Mongod     string = "127.0.0.1"
	DBName     string = "test"
	Collection string = "temperature"
)

type ClientConn struct {
	websocket *websocket.Conn
	clientIP  net.Addr
	RoomId    string
}

type Temperature struct {
	Id       bson.ObjectId `bson:"_id,omitempty"`
	Temp     float64
	Pressure float64
	Altitude float64
	Datetime string
}

func addClient(cc ClientConn) {
	ActiveClientRWMutex.Lock()
	if Rooms[cc.RoomId] == nil {
		Rooms[cc.RoomId] = make(map[ClientConn]int)
	}
	Rooms[cc.RoomId][cc] = 0
	ActiveClientRWMutex.Unlock()

}

func deleteClient(cc ClientConn) {
	ActiveClientRWMutex.Lock()
	delete(Rooms[cc.RoomId], cc)
	ActiveClientRWMutex.Unlock()
}

func broadcastMessage(messageType int, message []byte, roomId string) {
	ActiveClientRWMutex.RLock()
	defer ActiveClientRWMutex.RUnlock()

	for client, _ := range Rooms[roomId] {
		if err := client.websocket.WriteMessage(messageType, message); err != nil {
			return
		}
	}

}

func getSendJson() (jsonString string, err error) {
	jsonString = ""
	data, err := getMongoData()
	if err != nil {
		return
	}
	b, err := json.Marshal(data)
	if err != nil {
		return
	}
	jsonString = string(b)
	return
}

func getMongoData() ([]Temperature, error) {
	session, err := mgo.Dial(Mongod)
	if err != nil {
		return nil, err
	}
	defer session.Close()
	session.SetMode(mgo.Monotonic, true)
	conn := session.DB(DBName).C(Collection)

	iter := conn.Find(nil).Sort("-$natural").Limit(12).Iter()

	result := Temperature{}
	var tpr []Temperature
	for iter.Next(&result) {
		tpr = append(tpr, result)
	}
	return tpr, nil
}

func main() {
	m := martini.Classic()
	m.Use(render.Renderer())
	m.Use(martini.Static("static"))
	m.Get("/", Index)
	m.Get("/ws", WebSocket)

	go func() {
		for {
			time.Sleep(10 * time.Second)
			json, err := getSendJson()
			if err != nil {
				log.Println(err)
			}
			broadcastMessage(1, []byte(json), GraphId)
		}
	}()
	m.Run()

}

func Index(r render.Render) {
	r.HTML(200, "graph", GraphId)
}

func WebSocket(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{ReadBufferSize: 1024, WriteBufferSize: 1024}

	ws, err := upgrader.Upgrade(w, r, nil)
	if _, ok := err.(websocket.HandshakeError); ok {
		http.Error(w, "not a websocket handshake", 400)
	} else if err != nil {
		log.Println(err)
		return
	}

	client := ws.RemoteAddr()
	clientConn := ClientConn{ws, client, GraphId}
	addClient(clientConn)

	for {
		messageType, p, err := ws.ReadMessage()
		if err != nil {
			deleteClient(clientConn)
			log.Println(err)
			return
		}
		log.Println(messageType, p)
		broadcastMessage(messageType, p, clientConn.RoomId)
	}
}
