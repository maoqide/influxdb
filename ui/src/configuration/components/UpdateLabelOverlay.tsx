// Libraries
import React, {Component, ChangeEvent} from 'react'
import _ from 'lodash'

// Components
import LabelOverlayForm from 'src/configuration/components/LabelOverlayForm'
import {Overlay, OverlayState} from 'src/clockface'
import {ComponentStatus} from '@influxdata/clockface'

// Types
import {ILabel} from '@influxdata/influx'

// Decorators
import {ErrorHandling} from 'src/shared/decorators/errors'

interface Props {
  label: ILabel
  onDismiss: () => void
  onUpdateLabel: (label: ILabel) => Promise<void>
  onNameValidation: (name: string) => string | null
  visible: OverlayState
}

interface State {
  label: ILabel
  colorStatus: ComponentStatus
}

@ErrorHandling
class UpdateLabelOverlay extends Component<Props, State> {
  constructor(props: Props) {
    super(props)

    this.state = {
      label: props.label,
      colorStatus: ComponentStatus.Default,
    }
  }

  public componentDidUpdate(prevProps: Props) {
    if (!_.isEqual(prevProps.label, this.props.label)) {
      this.setState({label: this.props.label})
    }
  }

  public render() {
    const {onDismiss, onNameValidation, visible} = this.props
    const {label} = this.state

    if (!label) {
      return null
    }

    return (
      <Overlay maxWidth={400} visible={visible}>
        <Overlay.Heading title="Edit Label" onDismiss={onDismiss} />
        <Overlay.Body>
          <LabelOverlayForm
            id={label.id}
            name={label.name}
            description={label.properties.description}
            color={label.properties.color}
            onColorChange={this.handleColorHexChange}
            onSubmit={this.handleSubmit}
            onCloseModal={onDismiss}
            onInputChange={this.handleInputChange}
            buttonText="Save Changes"
            isFormValid={this.isFormValid}
            onNameValidation={onNameValidation}
          />
        </Overlay.Body>
      </Overlay>
    )
  }

  private get isFormValid(): boolean {
    const {label, colorStatus} = this.state

    const nameIsValid = this.props.onNameValidation(label.name) === null
    const colorIsValid =
      colorStatus === ComponentStatus.Default ||
      colorStatus == ComponentStatus.Valid

    return nameIsValid && colorIsValid
  }

  private handleSubmit = () => {
    const {onUpdateLabel, onDismiss} = this.props

    onUpdateLabel(this.state.label)
    onDismiss()
  }

  private handleInputChange = (e: ChangeEvent<HTMLInputElement>): void => {
    const value = e.target.value
    const key = e.target.name

    if (key === 'description' || key === 'color') {
      const properties = {...this.state.label.properties, [key]: value}
      const label = {...this.state.label, properties}

      this.setState({
        label,
      })
    } else {
      const label = {...this.state.label, [key]: value}

      this.setState({
        label,
      })
    }
  }

  private handleColorHexChange = (
    color: string,
    colorStatus: ComponentStatus
  ): void => {
    const properties = {...this.state.label.properties, color}
    const label = {...this.state.label, properties}

    this.setState({label, colorStatus})
  }
}

export default UpdateLabelOverlay
