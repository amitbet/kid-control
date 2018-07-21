package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/amitbet/KidControl/config"

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
	address := cfg.ServiceAddress
	if strings.HasPrefix(address, ":") {
		address = "http://localhost" + address
	}

	if !strings.HasPrefix(strings.ToLower(address), "http://") {
		address = "http://" + address
	}
	machineIp := cfg.MachineList[machine]
	u, _ := url.Parse(address)
	return "http://" + machineIp + ":" + u.Port()
}

func setVolumeOnMachine(wr http.ResponseWriter, req *http.Request) {

	vars := gmux.Vars(req)
	machine := vars["machine"]
	if machine == "" {
		getVolume(wr, req)
		return
	}
	murl := prepareMachineUrl(machine)

	res, err := http.Post(murl+"/set-volume", "application/json", req.Body)
	if err != nil {
		logger.Error("setVolumeOnMachine Error getting volume from remote machine: ", err)
		return
	}
	if res.StatusCode != 200 {
		logger.Error("setVolumeOnMachine Error getting volume from remote machine: ", res.StatusCode, res.Status)
		return
	}

}

func getVolumeOnMachine(wr http.ResponseWriter, req *http.Request) {

	vars := gmux.Vars(req)
	machine := vars["machine"]
	if machine == "" {
		getVolume(wr, req)
		return
	}

	murl := prepareMachineUrl(machine)

	res, err := http.Get(murl + "/get-volume")
	if err != nil {
		logger.Error("getVolumeOnMachine Error getting volume from remote machine: ", err)
		return
	}
	if res.StatusCode != 200 {
		logger.Error("getVolumeOnMachine Error getting volume from remote machine: ", res.StatusCode, res.Status)
		return
	}

	jsonStr, err := ioutil.ReadAll(res.Body)
	if err != nil {
		logger.Error("getVolumeOnMachine Error reading body: ", err)
		return
	}

	// jsonRes := map[string]interface{}{}
	// err = json.Unmarshal(jsonStr, &jsonRes)
	// if err != nil {
	// 	logger.Error("getVolumeOnMachine Error unmarshaling json: ", err)
	// 	return
	// }
	// vol := jsonRes["volume"].(int)
	// jObj := map[string]interface{}{
	// 	"volume": vol,
	// }
	//SendMessage(wr, jObj)
	logger.Debugf("getVolumeOnMachine relay: %d\n", string(jsonStr))

	wr.Write(jsonStr)
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
	// logger.Debugf("extractPatientSymptoms unmarshaled obj: %v", jObj)

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

func main() {
	fmt.Println("starting!")

	mux := gmux.NewRouter() //.StrictSlash(true)

	cfg := config.GetDefaultConfig()
	mux.HandleFunc("/set-volume", setVolume)
	mux.HandleFunc("/get-volume", getVolumeOnMachine).Queries("machine", "{machine}")
	mux.HandleFunc("/get-volume", getVolume)
	//mux.HandleFunc("/get-volume", getVolumeOnMachine)
	mux.PathPrefix("/").Handler(http.FileServer(http.Dir("./public")))
	logger.Info("Listening on address: ", cfg.ServiceAddress)
	err := http.ListenAndServe(cfg.ServiceAddress, mux)
	if err != nil {
		logger.Errorf("lisgtening error: ", err)
	}

}
