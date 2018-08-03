package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/amitbet/KidControl/config"
	ssdp "github.com/koron/go-ssdp"

	"github.com/amitbet/KidControl/logger"

	gmux "github.com/gorilla/mux"
	"github.com/itchyny/volume-go"
)

func SendMessage(wr http.ResponseWriter, message map[string]interface{}) {
	jsonStr, err := json.Marshal(message)
	if err != nil {
		logger.Errorf("SendMessage failed: %+v", err)
	}

	wr.Write([]byte(jsonStr))
}
func prepareMachineUrl(machine string) string {

	cfg := config.GetDefaultConfig()

	machineUrl := cfg.ServiceList[machine]
	if !strings.HasSuffix(machineUrl, "/") {
		machineUrl += "/"
	}
	return machineUrl
}

func setVolumeOnMachine(wr http.ResponseWriter, req *http.Request) {

	body, err := ioutil.ReadAll(req.Body)
	logger.Debug("setVolumeOnMachine got: ", string(body))
	if err != nil {
		logger.Error("error in reading req body: ", err)
	}
	bodyBuff := bytes.NewBuffer(body)

	vars := gmux.Vars(req)
	machine := vars["machine"]
	murl := prepareMachineUrl(machine)

	res, err := http.Post(murl+"set-volume", "application/json", bodyBuff)
	if err != nil {
		logger.Error("setVolumeOnMachine Error setting volume from remote machine: ", err)
		return
	}
	if res.StatusCode != 200 {
		logger.Error("setVolumeOnMachine Error setting volume from remote machine: ", res.StatusCode, res.Status)
		return
	}

}

type AsyncCallResponse struct {
	Body  []byte
	Url   string
	Error error
}

func asyncHttpGets(urls []string) []AsyncCallResponse {
	ch := make(chan AsyncCallResponse, len(urls)) // buffered
	responses := []AsyncCallResponse{}

	for _, url := range urls {
		go func(url string) {
			timeout := time.Duration(2 * time.Second)
			client := http.Client{
				Timeout: timeout,
			}
			fmt.Printf("Fetching %s \n", url)
			resp, err := client.Get(url)
			if err != nil {
				ch <- AsyncCallResponse{Body: []byte{}, Url: url, Error: err}
				return
			}
			body, err := ioutil.ReadAll(resp.Body)
			ch <- AsyncCallResponse{Body: body, Url: url, Error: err}
		}(url)
	}

	for {
		select {
		case r := <-ch:
			fmt.Printf("%s was fetched\n", r.Url)
			responses = append(responses, r)
			if len(responses) == len(urls) {
				return responses
			}
			// case <-time.After(50 * time.Millisecond):
			// 	fmt.Printf(".")
		}
	}

	return responses

}

func sendConfig(wr http.ResponseWriter, req *http.Request) {
	cfg := config.GetDefaultConfig()
	clientConfig := map[string]interface{}{
		"machines": []map[string]interface{}{},
	}
	machines := clientConfig["machines"].([]map[string]interface{})

	machineMap := map[string]string{}
	urls := []string{}
	for k, _ := range cfg.ServiceList {

		murl := prepareMachineUrl(k) + "/get-volume"
		machineMap[murl] = k
		urls = append(urls, murl)
	}

	results := asyncHttpGets(urls)
	for _, result := range results {
		parsed := map[string]interface{}{}
		if result.Error != nil {
			continue
		}
		//logger.Debug("sendConfig async get volumes got: ", string(result.Body))
		err := json.Unmarshal(result.Body, &parsed)
		if err != nil {
			logger.Error("sendConfig Error in unmarshaling results: ", err)
			continue
		}
		if parsed["volume"] == nil {
			continue
		}
		vol := parsed["volume"].(float64)
		//	vol, err := strconv.Atoi(volStr)

		machines = append(machines, map[string]interface{}{
			"name":   machineMap[result.Url],
			"volume": vol,
		})
	}

	clientConfig["machines"] = machines
	jStr, err := json.Marshal(clientConfig)
	if err != nil {
		logger.Error("sendConfig Error in marshaling: ", err)
		return
	}
	_, err = wr.Write(jStr)
	if err != nil {
		logger.Error("sendConfig Error in writing config to response: ", err)
		return
	}
}
func getMachineVol(machine string) int {
	if machine == "" {
		machine = "localhost"
	}
	timeout := time.Duration(2 * time.Second)
	client := http.Client{
		Timeout: timeout,
	}

	murl := prepareMachineUrl(machine)

	res, err := client.Get(murl + "get-volume")
	if err != nil {
		logger.Error("getVolumeOnMachine Error getting volume from remote machine: ", err)
		return -1
	}
	if res.StatusCode != 200 {
		logger.Error("getVolumeOnMachine Error getting volume from remote machine: ", res.StatusCode, res.Status)
		return -1
	}

	jsonStr, err := ioutil.ReadAll(res.Body)
	if err != nil {
		logger.Error("getVolumeOnMachine Error reading body: ", err)
		return -1
	}
	jObj := map[string]int{}
	json.Unmarshal(jsonStr, &jObj)

	return jObj["volume"]
}
func getVolumeOnMachine(wr http.ResponseWriter, req *http.Request) {

	vars := gmux.Vars(req)
	machine := vars["machine"]
	vol := getMachineVol(machine)
	jObj := map[string]interface{}{
		"volume": vol,
	}
	SendMessage(wr, jObj)
}

func getVolume(wr http.ResponseWriter, req *http.Request) {
	vol, err := volume.GetVolume()
	if err != nil {
		logger.Errorf("get volume failed: %+v", err)
	}
	jObj := map[string]interface{}{
		"volume": vol,
	}
	SendMessage(wr, jObj)
	logger.Debugf("sending volume: %d\n", vol)
}

func setVolume(wr http.ResponseWriter, req *http.Request) {
	body, err := ioutil.ReadAll(req.Body)
	logger.Debug("setVolume got: ", string(body))
	if err != nil {
		logger.Error("error in reading req body: ", err)
	}
	jObj := map[string]interface{}{}
	err = json.Unmarshal(body, &jObj)
	if err != nil {
		logger.Error("setVolume, error in unmarshaling body: ", err)
	}

	volStr := jObj["volume"].(string)
	vol, err := strconv.Atoi(volStr)
	if err != nil {
		logger.Errorf("parse volume failed: %+v", err)
	}
	err = volume.SetVolume(vol)
	if err != nil {
		logger.Errorf("set volume failed: %+v", err)
	}
	logger.Debugf("set volume success val=%d", vol)

	jObj1 := map[string]interface{}{
		"volume": vol,
	}
	SendMessage(wr, jObj1)
	logger.Debugf("sending volume back: %d\n", vol)
}

func ssdpAdvertise(quit chan bool) {
	myIp := getHostIp().String()
	hname, err := os.Hostname()
	if err != nil {
		logger.Error("Error getting hostname: ", err)
	}

	ad, err := ssdp.Advertise(
		"urn:schemas-upnp-org:service:KidControl:1", // send as "ST"
		"id:"+hname,                                 // send as "USN"
		"http://"+myIp+":7777/",                     // send as "LOCATION"
		"ssdp for KidControl",                       // send as "SERVER"
		3600) // send as "maxAge" in "CACHE-CONTROL"
	if err != nil {
		logger.Error("Error advertising ssdp: ", err)
	}

	//aliveTick := time.Tick(5 * time.Second)

	// run Advertiser infinitely.
	for {
		select {
		//case <-aliveTick:
		//ad.Alive()
		case <-quit:
			// send/multicast "byebye" message.
			ad.Bye()
			// teminate Advertiser.
			ad.Close()
			return
		}
	}
}
func ssdpSearch(searchType string, waitTime int, multicastSendAddress string, listenAddress string) []ssdp.Service {

	list, err := ssdp.Search(searchType, waitTime, listenAddress)
	if err != nil {
		logger.Error("Error while searching ssdp: ", err)
	}
	for i, srv := range list {
		//fmt.Printf("%d: %#v\n", i, srv)
		fmt.Printf("%d: %s %s\n", i, srv.Type, srv.Location)
	}
	return list
}

// func ssdpServer() {
// 	hname, err := os.Hostname()
// 	if err != nil {
// 		logger.Error("Error getting hostname: ", err)
// 	}

// 	s, err := gossdp.NewSsdp(nil)
// 	if err != nil {
// 		logger.Error("Error creating ssdp server: ", err)
// 		return
// 	}
// 	defer s.Stop()
// 	go s.Start()

// 	serverDef := gossdp.AdvertisableServer{
// 		ServiceType: "urn:kidcontrol:test:web:1",
// 		DeviceUuid:  hname, //"hh0c2981-0029-44b7-4u04-27f187aecf78",
// 		Location:    "http://192.168.1.1:8080",
// 		MaxAge:      3600,
// 	}
// 	s.AdvertiseServer(serverDef)
// 	time.Sleep(5 * time.Second)
// }

// type blah struct {
// }

// func (b blah) NotifyAlive(message gossdp.AliveMessage) {
// 	logger.Debug("NotifyAlive %#v\n", message)
// }
// func (b blah) NotifyBye(message gossdp.ByeMessage) {
// 	logger.Debug("NotifyBye %#v\n", message)
// }
// func (b blah) Response(message gossdp.ResponseMessage) {
// 	logger.Debug("Response %#v\n", message)
// }

// func ssdpClient() {
// 	b := blah{}
// 	c, err := gossdp.NewSsdpClient(b)
// 	if err != nil {
// 		logger.Error("Failed to start client: ", err)
// 		return
// 	}
// 	//defer c.Stop()
// 	go c.Start()

// 	err = c.ListenFor("urn:kidcontrol:test:web:1")

// 	//.time.Sleep(15 * time.Second)
// }
func getHostIp() net.IP {
	host, _ := os.Hostname()
	addrs, _ := net.LookupIP(host)

	for _, addr := range addrs {
		if ipv4 := addr.To4(); ipv4 != nil && ipv4[0] == 192 {
			return ipv4
			//fmt.Println("IPv4: ", ipv4)
		}
	}
	return net.IP{}
}

func main() {
	fmt.Println("starting!")
	quit := make(chan bool)

	go ssdpAdvertise(quit)

	ssdpSearch("urn:schemas-upnp-org:service:KidControl:1", 5, "", "")
	//ssdpServer()
	//ssdpClient()
	mux := gmux.NewRouter() //.StrictSlash(true)

	cfg := config.GetDefaultConfig()
	mux.HandleFunc("/set-volume", setVolumeOnMachine).Queries("machine", "{machine}")
	mux.HandleFunc("/set-volume", setVolume)
	mux.HandleFunc("/get-volume", getVolumeOnMachine).Queries("machine", "{machine}")
	mux.HandleFunc("/get-volume", getVolume)
	mux.HandleFunc("/configuration", sendConfig)

	mux.PathPrefix("/").Handler(http.FileServer(http.Dir("./public")))
	logger.Info("Listening on address: ", cfg.ListeningAddress)
	err := http.ListenAndServe(cfg.ListeningAddress, mux)
	if err != nil {
		logger.Errorf("lisgtening error: ", err)
	}
}
