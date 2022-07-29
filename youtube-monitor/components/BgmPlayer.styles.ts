import { css } from "@emotion/react"


export const bgmPlayer = css`
    height: calc(1080px - 200px - 350px - 300px);
    width: 400px;
    background-color: #3535359f;
    position: absolute;
    right: 0;
    bottom: 300px;
    text-align: center;
    color: white;
    // overflow: hidden;
    word-break: break-all;
    z-index: 20;

    & h4 {
        text-align: right;
        margin-inline-end: 1rem;
    }
`

export const audioCanvasDiv = css`
    height: calc(1080px - 200px - 350px - 300px);
    width: 400px;
    background-color: #77777777;
    position: absolute;
    right: 0;
    bottom: 300px;
    // text-align: center;
    // color: white;
    // overflow: hidden;
    z-index: 10;
`

export const audioCanvas = css`
    height: calc(1080px - 200px - 350px - 300px - 150px);
    width: 400px;
    background-color: #77777700;
    position: absolute;
    right: 0;
    bottom: 0;
    // text-align: center;
    // color: white;
    // overflow: hidden;
    z-index: 15;
`