// Libraries
import React, {PureComponent} from 'react'

// Styles
import 'src/organizations/components/CreateVariableOverlay.scss'

// Components
import {OverlayBody, OverlayHeading, OverlayContainer} from 'src/clockface'
import VariableForm from 'src/organizations/components/VariableForm'

// Types
import {Variable} from '@influxdata/influx'

interface Props {
  onCreateVariable: (variable: Variable) => void
  onHideOverlay: () => void
  orgID: string
  initialScript?: string
}

export default class CreateVariableOverlay extends PureComponent<Props> {
  public render() {
    const {onHideOverlay, onCreateVariable, orgID, initialScript} = this.props

    return (
      <OverlayContainer maxWidth={1000}>
        <OverlayHeading title="Create Variable" onDismiss={onHideOverlay} />
        <OverlayBody>
          <VariableForm
            onCreateVariable={onCreateVariable}
            onHideOverlay={onHideOverlay}
            orgID={orgID}
            initialScript={initialScript}
          />
        </OverlayBody>
      </OverlayContainer>
    )
  }
}
