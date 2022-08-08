import { Global } from '@emotion/react'
import type { AppProps } from 'next/app'
import { globalStyle } from '../styles/global.styles'

function MyApp({ Component, pageProps }: AppProps): JSX.Element {
    return (
        <>
            <Global styles={globalStyle} />
            <Component {...pageProps} />
        </>
    )
}

export default MyApp
