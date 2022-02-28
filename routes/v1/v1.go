package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/websocket/v2"
	"github.com/xornet-cloud/Backend/apierrors"
	"github.com/xornet-cloud/Backend/auth"
	"github.com/xornet-cloud/Backend/database"
	"github.com/xornet-cloud/Backend/logic"
	"github.com/xornet-cloud/Backend/middleware"
	"github.com/xornet-cloud/Backend/types"
	"github.com/xornet-cloud/Backend/validators"
	"go.mongodb.org/mongo-driver/bson"
)

type V1 struct {
	db database.Database
}

type WebsocketEventName struct {
	Name string `json:"e"`
}

type ClientLoginEvent struct {
	WebsocketEventName
	Data ClientLoginData `json:"d"`
}

type MachineDynamicDataEvent struct {
	WebsocketEventName
	Data types.MachineDynamicData `json:"d"`
}

type ReporterStaticDataEvent struct {
	WebsocketEventName
	Data database.MachineStaticData `json:"d"`
}

type ClientLoginData struct {
	AuthToken string `json:"auth_token"`
}

type ClientDynamicData struct {
	UUID             string                        `json:"uuid"`
	CPU              types.CPUStats                `json:"cpu"`
	RAM              types.RAMStats                `json:"ram"`
	GPU              types.GPUStats                `json:"gpu"`
	Disks            []types.DiskStats             `json:"disks"`
	Temps            []types.TempStats             `json:"temps"`
	Network          []types.NetworkInterfaceStats `json:"network"`
	ProcessCount     uint64                        `json:"process_count"`
	HostUptime       uint64                        `json:"host_uptime"`
	ReporterUptime   uint64                        `json:"reporter_uptime"`
	CpuAverageUsage  float32                       `json:"cau"`
	CpuAverageSpeed  float32                       `json:"cas"`
	TotalTrafficDown float32                       `json:"td"`
	TotalTrafficUp   float32                       `json:"tu"`
}

type ClientDynamicDataEvent struct {
	Name string            `json:"e"`
	Data ClientDynamicData `json:"d"`
}

func (v1 V1) getDocByFieldFromParam(c *fiber.Ctx, docType string, paramName string) error {
	paramValue := c.Params(paramName)
	if !validators.IsNotEmpty(paramValue) {
		return apierrors.ParamInvalidError
	}

	var filter = bson.M{paramName: paramValue}

	if docType == "users" {
		doc, err := v1.db.GetUser(c.Context(), filter)
		if err != nil {
			return apierrors.UserNotFoundError
		}
		return c.JSON(&doc)
	} else if docType == "machines" {
		doc, err := v1.db.GetMachine(c.Context(), filter)
		if err != nil {
			return apierrors.MachineNotFoundError
		}
		return c.JSON(&doc)
	}

	return apierrors.ParamInvalidError
}

func New(db database.Database, app *fiber.App) V1 {
	var userMiddleware = middleware.UserMiddleware(&db)
	var keyManager = auth.NewKeyManager()
	var clients = make(map[string]websocket.Conn)
	var reporters = make(map[string]*websocket.Conn)
	var machineBuffer []ClientDynamicData

	// Increment the WaitGroup counter.
	heartbeatClock := time.NewTicker(5 * time.Second)
	machineDataClock := time.NewTicker(time.Second)

	go func() {
		for {
			select {
			case <-heartbeatClock.C:
				// Send heartbeats to all the clients
				for _, socket := range clients {
					// Send a heartbeat
					socket.WriteJSON(WebsocketEventName{Name: "heartbeat"})
				}
			case <-machineDataClock.C:
				// Send heartbeats to all the clients
				for _, socket := range clients {
					// Send all the machines from the buffer
					for _, machine := range machineBuffer {
						socket.WriteJSON(ClientDynamicDataEvent{
							Name: "machineData",
							Data: machine,
						})
					}
				}
				// Remove the current element
				if len(machineBuffer) != 0 {
					machineBuffer = machineBuffer[1:]
				}
			}
		}
	}()

	var v1 = V1{db}
	var v = "/v1"

	app.Get("/client", websocket.New(func(c *websocket.Conn) {
		var message []byte
		var err error

		for {
			if _, message, err = c.ReadMessage(); err != nil {
				break
			}
			// Get the event name from the event
			var event WebsocketEventName
			json.Unmarshal([]byte(message), &event)

			switch event.Name {
			case "login":
				// Get the event data
				var data ClientLoginEvent
				// Parse the json data coming from the event
				json.Unmarshal([]byte(message), &data)
				// Get the user's uuid from their token
				uuid, _ := auth.GetUuidFromToken(data.Data.AuthToken)
				// Set this websocket to the hashmap with the users uuid
				clients[uuid+"-"+fmt.Sprint(logic.MakeTimestamp())] = *c
			}
		}
	}))

	app.Get("/reporter", websocket.New(func(c *websocket.Conn) {
		var message []byte
		var err error
		var uuid *string

		for {
			if _, message, err = c.ReadMessage(); err == nil {
				// Get the event name from the event
				var event WebsocketEventName
				json.Unmarshal([]byte(message), &event)

				switch event.Name {
				case "login":
					// Get the event data
					var data ClientLoginEvent

					// Parse the json data coming from the event
					json.Unmarshal([]byte(message), &data)

					// Get the user's uuid from their token
					id, _ := auth.GetUuidFromToken(data.Data.AuthToken)
					// TODO: Check if this id is in the database and if not close the socket
					uuid = &id
					// Set this websocket to the hashmap with the users uuid
					reporters[*uuid] = c
				case "staticData":
					var data ReporterStaticDataEvent
					json.Unmarshal([]byte(message), &data)
					db.UpdateStaticData(context.TODO(), *uuid, data.Data)
				case "dynamicData":
					var data MachineDynamicDataEvent
					json.Unmarshal([]byte(message), &data)
					var totalTx, totalRx = logic.GetTotalTraffic(data.Data.Network)
					var CpuAverageUsage = logic.GetAverageSumFromArray(data.Data.CPU.Usage)
					var CpuAverageSpeed = logic.GetAverageSumFromArray(data.Data.CPU.Freq)
					// Write to the buffer

					machineBuffer = append(machineBuffer, (ClientDynamicData{
						UUID:             *uuid,
						CPU:              data.Data.CPU,
						RAM:              data.Data.RAM,
						GPU:              data.Data.GPU,
						ProcessCount:     data.Data.ProcessCount,
						Disks:            data.Data.Disks,
						Temps:            data.Data.Temps,
						Network:          data.Data.Network,
						HostUptime:       data.Data.HostUptime,
						ReporterUptime:   data.Data.ReporterUptime,
						CpuAverageUsage:  CpuAverageUsage,
						CpuAverageSpeed:  CpuAverageSpeed,
						TotalTrafficUp:   totalTx,
						TotalTrafficDown: totalRx,
					}))
				}
			} else {
				break
			}
		}
	}))

	app.Get(v+"/ping", v1.Ping)
	app.Get(v+"/status", v1.Status)

	app.Post(v+"/auth/user/login", v1.LoginUser)
	app.Post(v+"/auth/user/signup", v1.SignupUser)
	app.Post(v+"/auth/machine/signup", func(c *fiber.Ctx) error { return v1.SignupMachine(c, keyManager) })

	app.Get(v+"/users/all", v1.GetUsersAll)
	app.Get(v+"/users/uuid/:uuid", v1.GetUserByUuid)
	app.Get(v+"/users/email/:email", v1.GetUserByEmail)
	app.Get(v+"/users/username/:username", v1.GetUserByUsername)
	app.Get(v+"/users/me", userMiddleware, v1.GetMe)
	app.Get(v+"/users/me/machines", userMiddleware, v1.GetMeMachines)
	app.Patch(v+"/users/me/avatar", userMiddleware, v1.UpdateAvatar)

	app.Get(v+"/machines/all", userMiddleware, v1.GetMachinesAll)
	app.Get(v+"/machines/uuid/:uuid", userMiddleware, v1.GetMachineByUuid)
	app.Get(v+"/machines/hostname/:hostname", userMiddleware, v1.GetMachineByHostname)
	app.Get(v+"/machines/owner/:owner", userMiddleware, v1.GetMachineByOwner)

	app.Get(v+"/machines/key", userMiddleware, func(c *fiber.Ctx) error { return v1.GenerateSignupToken(c, keyManager) })
	app.Delete(v+"/machines/uuid/:uuid", userMiddleware, v1.DeleteMachine)

	return v1
}