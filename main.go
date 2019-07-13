package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/amitbet/KidControl/config"
	"github.com/grandcat/zeroconf"
	ssdp "github.com/koron/go-ssdp"

	"github.com/amitbet/KidControl/logger"

	"github.com/amitbet/volume-go"
	gmux "github.com/gorilla/mux"
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
	if machine == "localhost" || machine == "127.0.0.1" {
		parsed := map[string]interface{}{}
		err := json.Unmarshal(body, &parsed)
		volStr := parsed["volume"].(string)
		vol, err := strconv.Atoi(volStr)

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
		return
	}
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

// NameSorter sorts planets by name.
type NameSorter []map[string]interface{}

func (a NameSorter) Len() int           { return len(a) }
func (a NameSorter) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a NameSorter) Less(i, j int) bool { return a[i]["name"].(string) < a[j]["name"].(string) }

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
		sort.Sort(NameSorter(machines))
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
	// vol, err := volume.GetVolume()
	// if err != nil {
	// 	logger.Errorf("get volume failed: %+v", err)
	// }
	jObj := map[string]interface{}{
		"volume": localVol,
	}
	SendMessage(wr, jObj)
	logger.Debugf("sending volume: %d\n", localVol)
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
		"id:"+hname,             // send as "USN"
		"http://"+myIp+":7777/", // send as "LOCATION"
		"ssdp for KidControl",   // send as "SERVER"
		3600)                    // send as "maxAge" in "CACHE-CONTROL"
	if err != nil {
		logger.Error("Error advertising ssdp: ", err)
	}

	aliveTick := time.Tick(5 * time.Second)

	// run Advertiser infinitely.
	for {
		select {
		case <-aliveTick:
			ad.Alive()
		case <-quit:
			logger.Info("Closing ssdp service")
			// send/multicast "byebye" message.
			ad.Bye()
			// teminate Advertiser.
			ad.Close()
			return
		}
	}
}
func ssdpSearch(searchType string, waitTime int, listenAddress string) []ssdp.Service {

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
func zconfRegister(quit chan bool) {
	myIp := getHostIp().String()
	hname, err := os.Hostname()
	meta := []string{
		"version=0.1.0",
		"ip=" + myIp,
	}
	if hname == "" {
		hname = myIp
	}

	service, err := zeroconf.Register(
		hname,              // service instance name
		"vol-control._tcp", // service type and protocol
		"local.",           // service domain
		7777,               // service port
		meta,               // service metadata
		nil,                // register on all network interfaces
	)

	if err != nil {
		log.Fatal(err)
	}
	select {
	case <-quit:
		logger.Info("stopping zeroconf publishing server")
		return
	}
	defer service.Shutdown()
	logger.Info("stopping zeroconf publishing server")
}

func zconfDiscover(serviceMap map[string]string) {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		log.Fatal(err)
	}

	// Channel to receive discovered service entries
	entries := make(chan *zeroconf.ServiceEntry)

	go func(results <-chan *zeroconf.ServiceEntry) {
		for entry := range results {
			logger.Debugf("Found service:, %s desc: %v address: %v:%s", entry.Instance, entry.Text, entry.AddrIPv4, strconv.Itoa(entry.Port))
			svcInstance := strings.ToLower(entry.Instance)
			var ip string
			for _, prop := range entry.Text {
				if strings.HasPrefix(prop, "ip=") {
					ip = prop[3:]
				}
			}
			if ip == "" {
				ip = entry.AddrIPv4[0].String()
			}
			svcUrl := "http://" + ip + ":" + strconv.Itoa(entry.Port) + "/"
			logger.Debugf("instance: %s svcUrl: %s", svcInstance, svcUrl)
			serviceMap[svcInstance] = svcUrl
		}
	}(entries)

	ctx := context.Background()

	err = resolver.Browse(ctx, "vol-control._tcp", "local.", entries)
	if err != nil {
		log.Fatalln("Failed to browse:", err.Error())
	}

	<-ctx.Done()
}

var volPollTimer *time.Ticker
var localVol int

func pollVolume() {
	var err error
	volPollTimer = time.NewTicker(time.Second)
	go func() {
		for {
			<-volPollTimer.C
			localVol, err = volume.GetVolume()
			if err != nil {
				logger.Errorf("listening error: ", err)
			}
		}
	}()
}

func main() {
	pollVolume()
	fmt.Println("starting!")
	cfg := config.GetDefaultConfig()
	var sigTerm = make(chan os.Signal)
	quit := make(chan bool)
	signal.Notify(sigTerm, syscall.SIGTERM)
	signal.Notify(sigTerm, syscall.SIGINT)
	go func() {
		sig := <-sigTerm
		fmt.Printf("caught sig: %+v\n", sig)
		fmt.Println("Waiting for a second to finish processing")
		quit <- true
		time.Sleep(1 * time.Second)
		os.Exit(0)
	}()

	//go zconfRegister(quit)
	//go zconfDiscover(cfg.ServiceList)

	go ssdpAdvertise(quit)
	svcList := ssdpSearch("urn:schemas-upnp-org:service:KidControl:1", 5, "")
	for _, svc := range svcList {
		svcName := svc.USN[3:]
		svcUrl := svc.Location
		cfg.ServiceList[strings.ToLower(svcName)] = svcUrl
	}

	mux := gmux.NewRouter() //.StrictSlash(true)

	mux.HandleFunc("/set-volume", setVolumeOnMachine).Queries("machine", "{machine}")
	mux.HandleFunc("/set-volume", setVolume)
	mux.HandleFunc("/get-volume", getVolumeOnMachine).Queries("machine", "{machine}")
	mux.HandleFunc("/get-volume", getVolume)
	mux.HandleFunc("/configuration", sendConfig)

	mux.PathPrefix("/").Handler(http.FileServer(http.Dir("./public")))
	logger.Info("Listening on address: ", cfg.ListeningAddress)
	err := http.ListenAndServe(cfg.ListeningAddress, mux)
	if err != nil {
		logger.Errorf("listening error: ", err)
	}
}
