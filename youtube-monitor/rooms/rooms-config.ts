import { RoomLayout } from "../types/room-layout";
import { circleRoomLayout } from "./layouts/circle_room";
import { classRoomLayout } from "./layouts/classroom";
import { HimajinRoomLayout } from "./layouts/himajin_room";
import { iLineRoomLayout } from "./layouts/iline_room";
import { mazeRoomLayout } from "./layouts/maze_room";
import { oneSeatRoomLayout } from "./layouts/one_seat_room";
import { SeaOfSeatRoomLayout } from "./layouts/sea_of_seat_room";
import { SimpleRoomLayout } from "./layouts/simple_room";
import { takochanRoomLayout } from "./layouts/takochan_room";
import { ver2RoomLayout } from "./layouts/ver2";

type RoomsConfig = {
    roomLayouts: RoomLayout[];

}

export const basicRooms: RoomsConfig = {
    roomLayouts: [circleRoomLayout, mazeRoomLayout, HimajinRoomLayout, takochanRoomLayout, SeaOfSeatRoomLayout]
}

export const temporaryRooms: RoomsConfig = {
    roomLayouts: [classRoomLayout, SimpleRoomLayout, mazeRoomLayout, HimajinRoomLayout]
}


export const numSeatsInAllBasicRooms = (): number => {
    let numSeatsBasicRooms = 0
    for (const r of basicRooms.roomLayouts) {
        numSeatsBasicRooms += r.seats.length
    }
    return numSeatsBasicRooms
}

