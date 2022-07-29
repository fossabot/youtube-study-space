import { css } from '@emotion/react'

export const roomLayout = css`
  position: relative;
  top: 0;
  left: 0;
  width: 100%;
  height: calc(1080px - 80px);
  box-sizing: border-box;
  margin: auto;
  border: solid 6px #303030;
`

export const seat = css`
  position: absolute;
  display: flex;
  flex-direction: column;
  justify-content: center;
  align-items: center;
`

export const partition = css`
  position: absolute;
  background-color: #2d2b41;
`

export const seatId = css`
  margin: 0;
  position: relative;
  top: 0;
  left: 0;
  color: #414141;
`

export const emptySeatNum = css`
  margin: 0;
  color: #414141;
`
export const usedSeatNum = css`

`

export const workName = css`
  margin: 0;
  color: #24317e;
  max-width: 100%;
  text-overflow: ellipsis;
  overflow: hidden;
  white-space: nowrap;
  font-weight: bolder;
`

export const userDisplayName = css`
  margin: 0;
  max-width: 100%;
  text-overflow: ellipsis;
  overflow: hidden;
  white-space: nowrap;
  color: #202020;
`
