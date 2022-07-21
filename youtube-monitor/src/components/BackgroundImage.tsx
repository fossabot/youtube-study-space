import { FC, useEffect, useRef, useState } from 'react'
import { getCurrentSection } from '../lib/time_table'
import * as styles from '../styles/BackgroundImage.styles'

const BackgroundImage: FC = () => {
    const base_url = 'https://source.unsplash.com/featured/1920x1080'
    const args: string =
        '/?' +
        'work,cafe,study,nature,chill,coffee,tea,sea,lake,outdoor,land,spring,summer,fall,winter,hotel' +
        ',green,purple,pink,blue,dark,azure,yellow,orange,gray,brown,red,black,pastel' +
        ',blossom,flower,corridor,door,background,wood,resort,travel,vacation,beach,grass' +
        ',pencil,pen,eraser,stationary,classic,jazz,lo-fi,fruit,vegetable,sky,instrument,cup' +
        ',star,moon,night,cloud,rain,mountain,river,calm,sun,sunny,water,building,drink,keyboard' +
        ',morning,evening'
    const unsplash_url = base_url + args

    const intervalRef = useRef<NodeJS.Timeout>()
    const [srcUrl, setSrcUrl] = useState<string>('')
    const [lastPartName, setLastPartName] = useState<string>()

    useEffect(() => {
        intervalRef.current = setInterval(() => {
            updateState()
        }, 1000)

        setSrcUrl(unsplash_url)
        setLastPartName('')
    }, []) // マウント時のみ

    useEffect(() => {
        // do something
        console.log()
        return () => {
            if (intervalRef.current) {
                clearInterval(intervalRef.current)
            }
        }
    }, [])

    const updateState = () => {
        const now = new Date()
        const currentSection = getCurrentSection()

        if (currentSection?.partType !== lastPartName) {
            setSrcUrl(`${unsplash_url},${now.getTime()}`)
            setLastPartName(currentSection?.partType)
        }
    }

    return (
        <div css={styles.backgroundImage}>
            <img src={srcUrl} alt='背景画像' />
        </div>
    )
}

export default BackgroundImage
