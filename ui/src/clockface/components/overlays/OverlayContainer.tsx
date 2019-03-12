// Libraries
import React, {SFC} from 'react'

interface Props {
  children: JSX.Element | JSX.Element[]
  maxWidth: number
  testID: string
}

const OverlayContainer: SFC<Props> = ({children, maxWidth, testID}) => {
  const style = {maxWidth: `${maxWidth}px`}

  return (
    <div
      className="overlay--container"
      data-testid={`${testID}--container`}
      style={style}
    >
      {children}
    </div>
  )
}

export default OverlayContainer
