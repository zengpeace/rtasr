package main

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"

	"golang.org/x/net/websocket"
)

var HOST = "rtasr.xfyun.cn/v1/ws"

var APPID = ""
var APPKEY = ""

// 结束标识
var ENDTAG = "{\"end\": true}"

// 每次发送的数据大小
var SLICESIZE = 1280

var PORT = 8888
var NoticeStopPort = 8889
var SendIntervalMs = 40 * time.Millisecond

var Running = false

func main() {
	socket, err := net.ListenUDP("udp4", &net.UDPAddr{
		IP:   net.IPv4(127, 0, 0, 1),
		Port: NoticeStopPort,
	})
	defer socket.Close()

	if err != nil {
		fmt.Println("listen port ", NoticeStopPort, " fail !")
		return
	}

	go func() {
		data := make([]byte, SLICESIZE*2)

		for {
			time.Sleep(2 * time.Second)
			recvDataBytes, _, err := socket.ReadFromUDP(data)
			if err != nil || recvDataBytes < 1 {
				continue
			}

			s := string(data[:])
			if s == "start" {
				Running = true
				runOne()
			} else if s == "stop" {
				time.Sleep(2 * time.Second)
				Running = false
				time.Sleep(5 * time.Second)
			} else {
				fmt.Println("unknow message ", s)
			}
		}
	}()

	for {
		time.Sleep(10 * time.Second)
	}
}

func runOne() {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha1.New, []byte(APPKEY))
	strByte := []byte(APPID + ts)
	strMd5Byte := md5.Sum(strByte)
	strMd5 := fmt.Sprintf("%x", strMd5Byte)
	mac.Write([]byte(strMd5))
	signa := url.QueryEscape(base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	requestParam := "appid=" + APPID + "&ts=" + ts + "&signa=" + signa

	conn, err := websocket.Dial("ws://"+HOST+"?"+requestParam, websocket.SupportedProtocolVersion, "http://"+HOST)
	if err != nil {
		println("err: ", err)
		return
	}

	var message string
	websocket.Message.Receive(conn, &message)
	var m map[string]string
	err = json.Unmarshal([]byte(message), &m)
	println(message)
	if err != nil {
		println(err.Error())
		return
	} else if m["code"] != "0" {
		println("handshake fail!" + message)
		return
	}

	defer conn.Close()
	sendChan := make(chan int, 1)
	readChan := make(chan int, 1)
	defer close(sendChan)
	defer close(readChan)
	go send(conn, sendChan)
	go receive(conn, readChan)
	<-sendChan
	<-readChan
}

func send(conn *websocket.Conn, sendChan chan int) {
	// 分片上传音频
	defer func() {
		sendChan <- 1
	}()

	socket, err := net.ListenUDP("udp4", &net.UDPAddr{
		IP:   net.IPv4(127, 0, 0, 1),
		Port: PORT,
	})
	defer socket.Close()

	if err != nil {
		fmt.Println("listen port ", PORT, "fail !")
		return
	}

	internalChan := make(chan []byte, SLICESIZE*4)
	func() {
		audioData := make([]byte, SLICESIZE*4)

		for {
			if Running == false {
				break
			}
			time.Sleep(SendIntervalMs / 2)

			recvDataBytes, _, err := socket.ReadFromUDP(audioData)
			if err != nil {
				continue
			}

			internalChan <- audioData[0:recvDataBytes]
		}

		fmt.Println("exit read audio data thread !")
	}()

	curBytes := 0
	data := make([]byte, SLICESIZE*8)

	for {
		if Running == false {
			break
		}
		time.Sleep(SendIntervalMs)

		d := <-internalChan
		dLen := len(d)
		copy(data[curBytes:], d)
		curBytes += dLen
		if curBytes >= SLICESIZE {
			err = websocket.Message.Send(conn, data[0:SLICESIZE])
			copy(data[0:], data[SLICESIZE:curBytes])
			curBytes -= SLICESIZE
			if err != nil {
				fmt.Println("send websocket data fail !")
				continue
			}
		}
	}

	// 上传结束符
	if err := websocket.Message.Send(conn, ENDTAG); err != nil {
		println("send string msg err: ", err)
	} else {
		println("send end tag success, ", len(ENDTAG))
	}
}

func receive(conn *websocket.Conn, readChan chan int) {
	for {
		if Running == false {
			break
		}

		var msg []byte
		var result map[string]string
		if err := websocket.Message.Receive(conn, &msg); err != nil {
			if err.Error() == "EOF" {
				println("receive date end")
			} else {
				println("receive msg error: ", err.Error())
			}

			break
		}

		err := json.Unmarshal(msg, &result)
		if err != nil {
			println(string(msg))
			println("response json parse error")
			continue
		}

		if result["code"] == "0" {
			var asrResult AsrResult
			err := json.Unmarshal([]byte(result["data"]), &asrResult)
			if err != nil {
				println("parse asrResult error: " + err.Error())
				println("receive msg: ", string(msg))

				break
			}
			if asrResult.Cn.St.Type == "0" {
				println("------------------------------------------------------------------------------------------------------------------------------------")
				// 最终结果
				for _, wse := range asrResult.Cn.St.Rt[0].Ws {
					for _, cwe := range wse.Cw {
						print(cwe.W)
					}
				}
				println("\r\n------------------------------------------------------------------------------------------------------------------------------------")
			} else {
				for _, wse := range asrResult.Cn.St.Rt[0].Ws {
					for _, cwe := range wse.Cw {
						print(cwe.W)
					}
				}
				println()
			}
		} else {
			println("invalid result: ", string(msg))
		}
	}
	readChan <- 1
}

type AsrResult struct {
	Cn    Cn      `json:"cn"`
	SegID float64 `json:"seg_id"`
}

type Cn struct {
	St St `json:"st"`
}

type St struct {
	Bg   string      `json:"bg"`
	Ed   string      `json:"ed"`
	Type string      `json:"type"`
	Rt   []RtElement `json:"rt"`
}

type RtElement struct {
	Ws []WsElement `json:"ws"`
}

type WsElement struct {
	Wb float64     `json:"wb"`
	We float64     `json:"we"`
	Cw []CwElement `json:"cw"`
}

type CwElement struct {
	W  string `json:"w"`
	Wp string `json:"wp"`
}
