/*
 * @Author: ph4ntom
 * @Date: 2021-04-02 15:43:04
 * @LastEditors: ph4ntom
 * @LastEditTime: 2021-04-02 17:29:12
 */
package manager

import (
	"Stowaway/protocol"
	"fmt"
	"net"
)

const (
	S_NEWSOCKS = iota
	S_ADDTCPSOCKET
	S_GETNEWSEQ
	S_GETTCPDATACHAN
	S_GETUDPDATACHAN
	S_GETTCPDATACHAN_WITHOUTUUID
	S_GETUDPDATACHAN_WITHOUTUUID
	S_CLOSETCP
	S_GETUDPSTARTINFO
	S_UPDATEUDP
	S_GETSOCKSINFO
	S_CLOSESOCKS
)

type socksManager struct {
	socksSeq         uint64
	socksSeqMap      map[uint64]int
	socksMap         map[int]*socks
	SocksTCPDataChan chan interface{} // accept both data and fin mess
	SocksUDPDataChan chan *protocol.SocksUDPData
	SocksReady       chan bool

	TaskChan   chan *SocksTask
	ResultChan chan *socksResult
	Done       chan bool // try to avoid this situation: A routine ask to get chan -> after that a TCPFIN message come and closeTCP() is called to close chan -> routine doesn't know chan is closed,so continue to input message into it -> panic
}

type SocksTask struct {
	Mode    int
	UUIDNum int    // node uuidNum
	Seq     uint64 // seq

	SocksPort        string
	SocksUsername    string
	SocksPassword    string
	SocksTCPListener net.Listener
	SocksTCPSocket   net.Conn
	SocksUDPListener *net.UDPConn
}

type socksResult struct {
	OK      bool
	UUIDNum int

	SocksSeq    uint64
	TCPAddr     string
	SocksInfo   string
	TCPDataChan chan []byte
	UDPDataChan chan []byte
}

type socks struct {
	port     string
	username string
	password string
	listener net.Listener

	socksStatusMap map[uint64]*socksStatus
}

type socksStatus struct {
	isUDP bool
	tcp   *tcpSocks
	udp   *udpSocks
}

type tcpSocks struct {
	dataChan chan []byte
	conn     net.Conn
}

type udpSocks struct {
	dataChan chan []byte
	listener *net.UDPConn
}

func newSocksManager() *socksManager {
	manager := new(socksManager)

	manager.socksMap = make(map[int]*socks)
	manager.socksSeqMap = make(map[uint64]int)
	manager.SocksTCPDataChan = make(chan interface{}, 5)
	manager.SocksUDPDataChan = make(chan *protocol.SocksUDPData, 5)
	manager.SocksReady = make(chan bool, 1)

	manager.TaskChan = make(chan *SocksTask)
	manager.Done = make(chan bool)
	manager.ResultChan = make(chan *socksResult)

	return manager
}

func (manager *socksManager) run() {
	for {
		task := <-manager.TaskChan

		switch task.Mode {
		case S_NEWSOCKS:
			manager.newSocks(task)
		case S_ADDTCPSOCKET:
			manager.addSocksTCPSocket(task)
		case S_GETNEWSEQ:
			manager.getSocksSeq(task)
		case S_GETTCPDATACHAN:
			manager.getTCPDataChan(task)
		case S_GETUDPDATACHAN:
			manager.getUDPDataChan(task)
			<-manager.Done
		case S_GETTCPDATACHAN_WITHOUTUUID:
			manager.getTCPDataChanWithoutUUID(task)
			<-manager.Done
		case S_GETUDPDATACHAN_WITHOUTUUID:
			manager.getUDPDataChanWithoutUUID(task)
			<-manager.Done
		case S_CLOSETCP:
			manager.closeTCP(task)
		case S_GETUDPSTARTINFO:
			manager.getUDPStartInfo(task)
		case S_UPDATEUDP:
			manager.updateUDP(task)
		case S_GETSOCKSINFO:
			manager.getSocksInfo(task)
		case S_CLOSESOCKS:
			manager.closeSocks(task)
		}
	}
}

/**
 * @description: register a new socks;If manager.socksMap[task.UUIDNum] not exist(!ok),register a new one,otherwise return false
 * @param {*SocksTask} task
 * @return {*}
 */
func (manager *socksManager) newSocks(task *SocksTask) {
	if _, ok := manager.socksMap[task.UUIDNum]; !ok {
		manager.socksMap[task.UUIDNum] = new(socks)
		manager.socksMap[task.UUIDNum].port = task.SocksPort
		manager.socksMap[task.UUIDNum].username = task.SocksUsername
		manager.socksMap[task.UUIDNum].password = task.SocksPassword
		manager.socksMap[task.UUIDNum].socksStatusMap = make(map[uint64]*socksStatus)
		manager.socksMap[task.UUIDNum].listener = task.SocksTCPListener
		manager.ResultChan <- &socksResult{OK: true}
	} else {
		manager.ResultChan <- &socksResult{OK: false}
	}
}

/**
 * @description: add a new TCP conn;If manager.socksMap[task.UUIDNum] exist(ok),register a new conn,otherwise return false
 * @param {*SocksTask} task
 * @return {*}
 */
func (manager *socksManager) addSocksTCPSocket(task *SocksTask) {
	if _, ok := manager.socksMap[task.UUIDNum]; ok {
		manager.socksMap[task.UUIDNum].socksStatusMap[task.Seq] = new(socksStatus)
		manager.socksMap[task.UUIDNum].socksStatusMap[task.Seq].tcp = new(tcpSocks) // no need to check if socksStatusMap[task.Seq] exist,because it must exist
		manager.socksMap[task.UUIDNum].socksStatusMap[task.Seq].tcp.dataChan = make(chan []byte)
		manager.socksMap[task.UUIDNum].socksStatusMap[task.Seq].tcp.conn = task.SocksTCPSocket
		manager.ResultChan <- &socksResult{OK: true}
	} else {
		manager.ResultChan <- &socksResult{OK: false}
	}
}

/**
 * @description: receive a valid socks seq num
 * @param {*SocksTask} task
 * @return {*}
 */
func (manager *socksManager) getSocksSeq(task *SocksTask) {
	manager.socksSeqMap[manager.socksSeq] = task.UUIDNum
	manager.ResultChan <- &socksResult{SocksSeq: manager.socksSeq}
	manager.socksSeq++
}

/**
 * @description: get a TCP corresponding datachan;If manager.socksMap[task.UUIDNum] exist(ok),return the corresponding datachan,otherwise return false
 * @param {*SocksTask} task
 * @return {*}
 */
func (manager *socksManager) getTCPDataChan(task *SocksTask) {
	if _, ok := manager.socksMap[task.UUIDNum]; ok {
		manager.ResultChan <- &socksResult{
			OK:          true,
			TCPDataChan: manager.socksMap[task.UUIDNum].socksStatusMap[task.Seq].tcp.dataChan,
		}
	} else {
		manager.ResultChan <- &socksResult{OK: false}
	}
}

/**
 * @description: get a UDP corresponding datachan;If manager.socksMap[task.UUIDNum] exist(ok),return the corresponding datachan,otherwise return false
 * @param {*SocksTask} task
 * @return {*}
 */
func (manager *socksManager) getUDPDataChan(task *SocksTask) {
	if _, ok := manager.socksMap[task.UUIDNum]; ok {
		if _, ok := manager.socksMap[task.UUIDNum].socksStatusMap[task.Seq]; ok {
			manager.ResultChan <- &socksResult{
				OK:          true,
				UDPDataChan: manager.socksMap[task.UUIDNum].socksStatusMap[task.Seq].udp.dataChan,
			}
		} else {
			manager.ResultChan <- &socksResult{OK: false}
		}
	} else {
		manager.ResultChan <- &socksResult{OK: false}
	}
}

/**
 * @description: get a TCP corresponding datachan without requiring uuid;If manager.socksSeqMap[task.Seq] exist(ok),return the corresponding datachan,otherwise return false
 * @param {*SocksTask} task
 * @return {*}
 */
func (manager *socksManager) getTCPDataChanWithoutUUID(task *SocksTask) {
	if _, ok := manager.socksSeqMap[task.Seq]; !ok {
		manager.ResultChan <- &socksResult{OK: false}
		return
	}

	uuidNum := manager.socksSeqMap[task.Seq]
	// if "manager.socksSeqMap[task.Seq]" exist, "manager.socksMap[uuidNum]" must exist too
	if _, ok := manager.socksMap[uuidNum].socksStatusMap[task.Seq]; ok {
		manager.ResultChan <- &socksResult{
			OK:          true,
			TCPDataChan: manager.socksMap[uuidNum].socksStatusMap[task.Seq].tcp.dataChan,
		}
	} else {
		manager.ResultChan <- &socksResult{OK: false}
	}
}

/**
 * @description: get a UDP corresponding datachan without requiring uuid;If manager.socksSeqMap[task.Seq] exist(ok),return the corresponding datachan,otherwise return false
 * @param {*SocksTask} task
 * @return {*}
 */
func (manager *socksManager) getUDPDataChanWithoutUUID(task *SocksTask) {
	if _, ok := manager.socksSeqMap[task.Seq]; !ok {
		manager.ResultChan <- &socksResult{OK: false}
		return
	}

	uuidNum := manager.socksSeqMap[task.Seq]
	// manager.socksMap[uuidNum] must exist if manager.socksSeqMap[task.Seq] exist
	if _, ok := manager.socksMap[uuidNum].socksStatusMap[task.Seq]; ok {
		manager.ResultChan <- &socksResult{
			OK:          true,
			UDPDataChan: manager.socksMap[uuidNum].socksStatusMap[task.Seq].udp.dataChan,
		}
	} else {
		manager.ResultChan <- &socksResult{OK: false}
	}
}

// close TCP include close UDP,cuz UDP's control channel is TCP,if TCP broken,UDP is also forced to be shutted down
func (manager *socksManager) closeTCP(task *SocksTask) {
	if _, ok := manager.socksSeqMap[task.Seq]; !ok {
		return
	}

	uuidNum := manager.socksSeqMap[task.Seq]

	manager.socksMap[uuidNum].socksStatusMap[task.Seq].tcp.conn.Close() // socksStatusMap[task.Seq] must exist, no need to check(error)
	close(manager.socksMap[uuidNum].socksStatusMap[task.Seq].tcp.dataChan)

	if manager.socksMap[uuidNum].socksStatusMap[task.Seq].isUDP {
		manager.socksMap[uuidNum].socksStatusMap[task.Seq].udp.listener.Close()
		close(manager.socksMap[uuidNum].socksStatusMap[task.Seq].udp.dataChan)
	}

	delete(manager.socksMap[uuidNum].socksStatusMap, task.Seq)
}

func (manager *socksManager) getUDPStartInfo(task *SocksTask) {
	if _, ok := manager.socksSeqMap[task.Seq]; !ok {
		manager.ResultChan <- &socksResult{OK: false}
		return
	}

	uuidNum := manager.socksSeqMap[task.Seq]

	if _, ok := manager.socksMap[uuidNum].socksStatusMap[task.Seq]; ok {
		manager.ResultChan <- &socksResult{
			OK:      true,
			TCPAddr: manager.socksMap[uuidNum].socksStatusMap[task.Seq].tcp.conn.LocalAddr().(*net.TCPAddr).IP.String(),
			UUIDNum: uuidNum,
		}
	} else {
		manager.ResultChan <- &socksResult{OK: false}
	}
}

func (manager *socksManager) updateUDP(task *SocksTask) {
	if _, ok := manager.socksMap[task.UUIDNum]; ok {
		if _, ok := manager.socksMap[task.UUIDNum].socksStatusMap[task.Seq]; ok {
			manager.socksMap[task.UUIDNum].socksStatusMap[task.Seq].isUDP = true
			manager.socksMap[task.UUIDNum].socksStatusMap[task.Seq].udp = new(udpSocks)
			manager.socksMap[task.UUIDNum].socksStatusMap[task.Seq].udp.dataChan = make(chan []byte)
			manager.socksMap[task.UUIDNum].socksStatusMap[task.Seq].udp.listener = task.SocksUDPListener
			manager.ResultChan <- &socksResult{OK: true}
		} else {
			manager.ResultChan <- &socksResult{OK: false}
		}
	} else {
		manager.ResultChan <- &socksResult{OK: false}
	}
}

func (manager *socksManager) getSocksInfo(task *SocksTask) {
	if _, ok := manager.socksMap[task.UUIDNum]; ok {
		if manager.socksMap[task.UUIDNum].username == "" && manager.socksMap[task.UUIDNum].password == "" {
			info := fmt.Sprintf("\r\nSocks Info ---> ListenAddr: 0.0.0.0:%s    Username: <null>    Password: <null>",
				manager.socksMap[task.UUIDNum].port,
			)
			manager.ResultChan <- &socksResult{
				OK:        true,
				SocksInfo: info,
			}
		} else {
			info := fmt.Sprintf("\r\nSocks Info ---> ListenAddr: 0.0.0.0:%s    Username: %s    Password: %s",
				manager.socksMap[task.UUIDNum].port,
				manager.socksMap[task.UUIDNum].username,
				manager.socksMap[task.UUIDNum].password,
			)
			manager.ResultChan <- &socksResult{
				OK:        true,
				SocksInfo: info,
			}
		}
	} else {
		info := fmt.Sprint("\r\nSocks service isn't running!")
		manager.ResultChan <- &socksResult{
			OK:        false,
			SocksInfo: info,
		}
	}
}

func (manager *socksManager) closeSocks(task *SocksTask) {
	manager.socksMap[task.UUIDNum].listener.Close()
	for seq, status := range manager.socksMap[task.UUIDNum].socksStatusMap {
		status.tcp.conn.Close()
		close(status.tcp.dataChan)
		if status.isUDP {
			status.udp.listener.Close()
			close(status.udp.dataChan)
		}
		delete(manager.socksMap[task.UUIDNum].socksStatusMap, seq)
	}

	for seq, uuidNum := range manager.socksSeqMap {
		if uuidNum == task.UUIDNum {
			delete(manager.socksSeqMap, seq)
		}
	}

	delete(manager.socksMap, task.UUIDNum) // we delete corresponding "socksMap"
	manager.ResultChan <- &socksResult{OK: true}
}