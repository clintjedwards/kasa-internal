package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"strings"
	"time"

	term "github.com/nsf/termbox-go"
)

// plug is the representation of the keybinding and plug pairing
type plug struct {
	IPAddress string
	KeyMap    string
	Model     string
	Name      string
	On        bool
}

type system struct {
	command `json:"system"`
}

type command struct {
	info `json:"get_sysinfo"`
}

type info struct {
	Alias           string  `json:"alias,omitempty"`
	SoftwareVersion string  `json:"sw_veri,omitempty"`
	HardwareVersion string  `json:"hw_ver,omitempty"`
	Model           string  `json:"model,omitempty"`
	DeviceID        string  `json:"deviceId,omitempty"`
	OemID           string  `json:"oemId,omitempty"`
	HardwareID      string  `json:"hwId,omitempty"`
	Rssi            float64 `json:"rssi,omitempty"`
	Longitude       float64 `json:"longitude,omitempty"`
	Latitude        float64 `json:"latitude,omitempty"`
	Updating        int     `json:"updating,omitempty"`
	LEDOff          int     `json:"led_off,omitempty"`
	RelayState      int     `json:"relay_state,omitempty"`
	OnTime          int     `json:"on_time,omitempty"`
	ActiveMode      string  `json:"active_mode,omitempty"`
	IconHash        string  `json:"icon_hash,omitempty"`
	ErrorCode       int     `json:"err_code,omitempty"`
}

func main() {
	if len(os.Args) != 2 {
		fmt.Println("Usage: kasa-internal <ip>:<key>,<ip>:<key>")
		os.Exit(1)
	}

	err := term.Init()
	if err != nil {
		panic(err)
	}

	// mapping should be in the form: <ip addr>:<key>,<ip addr>:<key>
	mapping := os.Args[1]
	_ = processMapping(mapping)

	for {
		switch ev := term.PollEvent(); ev.Type {
		case term.EventKey:
			switch ev.Key {
			case term.KeyArrowLeft:
				fmt.Println("You pressed the left key")
			case term.KeyArrowDown:
				fmt.Println("You pressed the right key")
			case term.KeyCtrlC:
				return
			}
		}
	}
	//getSystemInfo(plugs...)
}

// This takes a long time.
func getSystemInfo(plugs ...*plug) {
	fmt.Println("Retrieving information for plugs; this might take a while")

	for _, plug := range plugs {
		info, err := plug.systemInfo()
		if err != nil {
			fmt.Println(err)
			return
		}

		plug.Name = info.Alias
		plug.Model = info.Model
		plug.On = int2bool(info.RelayState)
		fmt.Printf("Found plug: %s\n", plug.Name)
	}
}

func int2bool(r int) bool {
	return r == 1
}

func encrypt(plaintext string) []byte {
	n := len(plaintext)
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.BigEndian, uint32(n))
	ciphertext := []byte(buf.Bytes())

	key := byte(0xAB)
	payload := make([]byte, n)
	for i := 0; i < n; i++ {
		payload[i] = plaintext[i] ^ key
		key = payload[i]
	}

	for i := 0; i < len(payload); i++ {
		ciphertext = append(ciphertext, payload[i])
	}

	return ciphertext
}

func decrypt(ciphertext []byte) []byte {
	n := len(ciphertext)
	key := byte(0xAB)
	var nextKey byte
	for i := 0; i < n; i++ {
		nextKey = ciphertext[i]
		ciphertext[i] = ciphertext[i] ^ key
		key = nextKey
	}
	return ciphertext
}

func processMapping(m string) []*plug {
	mappingSlice := strings.Split(m, ",")

	plugs := []*plug{}

	for _, mapping := range mappingSlice {
		IPKeyPair := strings.Split(mapping, ":")
		plugs = append(plugs, &plug{
			IPAddress: IPKeyPair[0],
			KeyMap:    IPKeyPair[1],
		})
	}

	return plugs
}

func (p *plug) systemInfo() (system, error) {
	payload := `{"system":{"get_sysinfo":{}}}`
	data := encrypt(payload)
	reading, err := send(p.IPAddress, data)
	if err != nil {
		return system{}, err
	}

	var info system
	results := decrypt(reading[4:])
	err = json.Unmarshal(results, &info)
	if err != nil {
		return system{}, err
	}

	return info, nil
}

func (p *plug) toggle() (err error) {
	if p.On {
		err = p.turnOff()
		return
	}

	err = p.turnOn()
	return
}

func (p *plug) turnOn() (err error) {
	json := `{"system":{"set_relay_state":{"state":1}}}`
	data := encrypt(json)
	_, err = send(p.IPAddress, data)
	return
}

func (p *plug) turnOff() (err error) {
	json := `{"system":{"set_relay_state":{"state":0}}}`
	data := encrypt(json)
	_, err = send(p.IPAddress, data)
	return
}

func send(ip string, payload []byte) (data []byte, err error) {
	conn, err := net.DialTimeout("tcp", ip+":9999", time.Duration(10)*time.Second)
	if err != nil {
		fmt.Println("Cannot connect to plug:", err)
		data = nil
		return
	}

	_, err = conn.Write(payload)
	if err != nil {
		fmt.Println("Cannot connect to plug:", err)
		data = nil
		return
	}

	data, err = ioutil.ReadAll(conn)
	if err != nil {
		fmt.Println("Cannot read data from plug:", err)
	}
	return
}
