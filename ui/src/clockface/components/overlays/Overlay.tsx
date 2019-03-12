// Libraries
import React, {Component} from 'react'
import classnames from 'classnames'

// Components
import OverlayContainer from 'src/clockface/components/overlays/OverlayContainer'
import OverlayHeading from 'src/clockface/components/overlays/OverlayHeading'
import OverlayBody from 'src/clockface/components/overlays/OverlayBody'
import OverlayFooter from 'src/clockface/components/overlays/OverlayFooter'
import FancyScrollbar from 'src/shared/components/fancy_scrollbar/FancyScrollbar'

// Styles
import 'src/clockface/components/overlays/Overlay.scss'

// Decorators
import {ErrorHandling} from 'src/shared/decorators/errors'

interface PassedProps {
  children: JSX.Element | JSX.Element[]
  visible: OverlayState
}

interface DefaultProps {
  maxWidth?: number
  testID?: string
  customOverlay?: boolean
  scrollable?: boolean
  className?: string
}

type Props = PassedProps & DefaultProps

export enum OverlayState {
  Show = 'overlay--show',
  Hide = 'overlay--hide',
}

interface State {
  showChildren: boolean
}

@ErrorHandling
class Overlay extends Component<Props, State> {
  public static defaultProps: DefaultProps = {
    maxWidth: 800,
    testID: 'overlay',
    customOverlay: false,
    scrollable: true,
  }

  public static Container = OverlayContainer
  public static Heading = OverlayHeading
  public static Body = OverlayBody
  public static Footer = OverlayFooter

  public static getDerivedStateFromProps(props) {
    if (props.visible) {
      return {showChildren: true}
    }

    return {}
  }

  private animationTimer: number

  constructor(props: Props) {
    super(props)

    this.state = {
      showChildren: false,
    }
  }

  public componentDidUpdate(prevProps: Props) {
    if (
      prevProps.visible === OverlayState.Show &&
      this.props.visible === OverlayState.Hide
    ) {
      clearTimeout(this.animationTimer)
      this.animationTimer = window.setTimeout(this.hideChildren, 300)
    }
  }

  public render() {
    const {scrollable, testID} = this.props

    if (scrollable) {
      return (
        <FancyScrollbar
          className={this.className}
          thumbStartColor="#ffffff"
          thumbStopColor="#C9D0FF"
          autoHide={false}
        >
          <div
            className="overlay--transition"
            data-testid={`${testID}--children`}
          >
            {this.children}
          </div>
          <div className="overlay--mask" />
        </FancyScrollbar>
      )
    }

    return (
      <div className={this.className}>
        <div
          className="overlay--transition"
          data-testid={`${testID}--children`}
        >
          {this.children}
        </div>
        <div className="overlay--mask" />
      </div>
    )
  }

  private get children(): JSX.Element | JSX.Element[] {
    const {children, customOverlay, maxWidth, testID} = this.props
    const {showChildren} = this.state

    if (!showChildren) {
      return null
    }

    if (customOverlay) {
      return children
    }

    return (
      <OverlayContainer maxWidth={maxWidth} testID={testID}>
        {children}
      </OverlayContainer>
    )
  }

  private get className(): string {
    const {visible, className} = this.props

    return classnames('overlay', {
      show: visible === OverlayState.Show,
      [`${className}`]: className,
    })
  }

  private hideChildren = (): void => {
    this.setState({showChildren: false})
  }
}

export default Overlay
