package handler

import (
	"Stowaway/admin/topology"
	"Stowaway/global"
	"Stowaway/protocol"
	"time"
)

func LetKeep(topo *topology.Topology) {
	topoTask := &topology.TopoTask{
		Mode:    topology.GETUUID,
		UUIDNum: 0,
	}

	topo.TaskChan <- topoTask
	topoResult := <-topo.ResultChan
	uuid := topoResult.UUID

	topoTask = &topology.TopoTask{
		Mode: topology.GETROUTE,
		UUID: uuid,
	}
	topo.TaskChan <- topoTask
	topoResult = <-topo.ResultChan
	route := topoResult.Route

	for {
		time.Sleep(time.Duration(10) * time.Second)

		sMessage := protocol.NewDownMsg(global.G_Component.Conn, global.G_Component.Secret, global.G_Component.UUID)

		header := &protocol.Header{
			Sender:      protocol.ADMIN_UUID,
			Accepter:    uuid,
			MessageType: protocol.KEEP,
			RouteLen:    uint32(len([]byte(route))),
			Route:       route,
		}

		keepMess := &protocol.KeepMsg{
			Ping: 1,
		}

		protocol.ConstructMessage(sMessage, header, keepMess, false)
		sMessage.SendMessage()
	}
}
