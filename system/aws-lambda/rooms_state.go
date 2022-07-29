package main

import (
	"app.modules/aws-lambda/lambdautils"
	"app.modules/core"
	"app.modules/core/myfirestore"
	"context"
	"github.com/aws/aws-lambda-go/lambda"
	"log"
)

type RoomsResponseStruct struct {
	Result      string              `json:"result"`
	Message     string              `json:"message"`
	DefaultRoom myfirestore.RoomDoc `json:"default_room"`
	MaxSeats int `json:"max_seats"`
	MinVacancyRate float32 `json:"min_vacancy_rate"`
}

func Rooms() (RoomsResponseStruct, error) {
	log.Println("Rooms()")
	
	ctx := context.Background()
	clientOption, err := lambdautils.FirestoreClientOption()
	if err != nil {
		return RoomsResponseStruct{}, err
	}
	_system, err := core.NewSystem(ctx, clientOption)
	if err != nil {
		return RoomsResponseStruct{}, err
	}
	defer _system.CloseFirestoreClient()
	
	defaultRoom, err := _system.FirestoreController.RetrieveRoom(ctx)
	if err != nil {
		return RoomsResponseStruct{}, err
	}
	
	constants, err := _system.FirestoreController.RetrieveSystemConstantsConfig(ctx)
	if err != nil {
		return RoomsResponseStruct{}, err
	}
	
	return RoomsResponse(defaultRoom, constants.MaxSeats, constants.MinVacancyRate), nil
}

func RoomsResponse(defaultRoom myfirestore.RoomDoc, maxSeats int, minVacancyRate float32) RoomsResponseStruct {
	var apiResp RoomsResponseStruct
	apiResp.Result = lambdautils.OK
	apiResp.DefaultRoom = defaultRoom
	apiResp.MaxSeats = maxSeats
	apiResp.MinVacancyRate = minVacancyRate
	return apiResp
}

func main() {
	lambda.Start(Rooms)
}
