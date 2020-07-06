package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	MQTT "github.com/eclipse/paho.mqtt.golang"
	"github.com/karalabe/hid"
)

var (
	temperature float32
	co2         uint16
)

func getData(dev *hid.Device) error {
	buf := make([]byte, 8)

	i, err := dev.Read(buf)
	if err != nil {
		return err
	}

	if i != len(buf) {
		return errors.New("wrong read bug size")
	}

	decode(buf)
	return nil
}

func decode(buf []byte) {
	res := make([]byte, 8)

	var c byte
	for _, n := range [][]int{{0, 2}, {1, 4}, {3, 7}, {5, 6}} {
		c = buf[n[0]]
		buf[n[0]] = buf[n[1]]
		buf[n[1]] = c
	}

	var tmp = buf[7] << 5
	res[7] = (buf[6] << 5) | (buf[7] >> 3)
	res[6] = (buf[5] << 5) | (buf[6] >> 3)
	res[5] = (buf[4] << 5) | (buf[5] >> 3)
	res[4] = (buf[3] << 5) | (buf[4] >> 3)
	res[3] = (buf[2] << 5) | (buf[3] >> 3)
	res[2] = (buf[1] << 5) | (buf[2] >> 3)
	res[1] = (buf[0] << 5) | (buf[1] >> 3)
	res[0] = tmp | (buf[0] >> 3)

	var magicWord = []byte("Htemp99e")

	for i := 0; i < 8; i++ {
		res[i] -= (magicWord[i] << 4) | (magicWord[i] >> 4)
	}

	if res[3] != res[0]+res[1]+res[2] {
		return
	}

	var w uint16
	w = uint16(res[1])*256 + uint16(res[2])

	switch res[0] {
	case 0x42:
		temperature = float32(w)*0.0625 - 273.15
	case 0x50:
		co2 = w
	}
}

func sendMqtt(server, topic, user, password string) error {
	opts := MQTT.NewClientOptions()
	opts.AddBroker(server)
	opts.SetClientID("mqtt-go-sender")
	opts.SetUsername(user)
	opts.SetPassword(password)

	client := MQTT.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		return token.Error()
	}

	token := client.Publish(topic+"/temperature", 0, false, fmt.Sprintf("%.2f", temperature))
	token.Wait()
	token = client.Publish(topic+"/co2", 0, false, fmt.Sprintf("%d", co2))
	token.Wait()

	client.Disconnect(250)
	return nil
}

func main() {
	server := flag.String("server", "", "The broker URI. ex: tcp://192.168.0.1:1883")
	topic := flag.String("topic", "dadget/room", "Base topic to send to")
	user := flag.String("user", "", "The User (optional)")
	password := flag.String("password", "", "The password (optional)")

	flag.Parse()

	devs := hid.Enumerate(0x04d9, 0xa052)
	if len(devs) == 0 {
		fmt.Println("device not found")
		os.Exit(1)
	}

	dev, err := devs[0].Open()
	if err != nil {
		fmt.Printf("can't open device - %v\n", err)
		os.Exit(1)
	}

	time.AfterFunc(time.Second*30, func() {
		println("timeout...")
		os.Exit(2)
	})

	for temperature == 0 || co2 == 0 {
		if err := getData(dev); err != nil {
			fmt.Printf("error: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Printf("temp: %.2f\nco2: %d\n", temperature, co2)

	if *server != "" {
		if err := sendMqtt(*server, *topic, *user, *password); err != nil {
			fmt.Printf("error: %v\n", err)
		}
	}
}
