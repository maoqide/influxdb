// Libraries
import React, {PureComponent, ChangeEvent} from 'react'
import {connect} from 'react-redux'

// Components
import CreateLabelOverlay from 'src/configuration/components/CreateLabelOverlay'
import TabbedPageHeader from 'src/shared/components/tabbed_page/TabbedPageHeader'
import {
  Button,
  IconFont,
  ComponentSize,
  ComponentColor,
} from '@influxdata/clockface'
import {EmptyState, Input, InputType, OverlayState} from 'src/clockface'
import LabelList from 'src/configuration/components/LabelList'
import FilterList from 'src/shared/components/Filter'

// Actions
import {notify as notifyAction} from 'src/shared/actions/notifications'
import {createLabel, updateLabel, deleteLabel} from 'src/labels/actions'

// Utils
import {validateLabelUniqueness} from 'src/configuration/utils/labels'

// Types
import {AppState} from 'src/types/v2'
import {ILabel} from '@influxdata/influx'

// Decorators
import {ErrorHandling} from 'src/shared/decorators/errors'

interface StateProps {
  labels: ILabel[]
}

interface State {
  searchTerm: string
  isOverlayVisible: OverlayState
}

interface DispatchProps {
  notify: typeof notifyAction
  createLabel: typeof createLabel
  updateLabel: typeof updateLabel
  deleteLabel: typeof deleteLabel
}

type Props = DispatchProps & StateProps

@ErrorHandling
class Labels extends PureComponent<Props, State> {
  constructor(props) {
    super(props)

    this.state = {
      searchTerm: '',
      isOverlayVisible: OverlayState.Hide,
    }
  }

  public render() {
    const {labels} = this.props
    const {searchTerm, isOverlayVisible} = this.state

    return (
      <>
        <TabbedPageHeader>
          <Input
            icon={IconFont.Search}
            widthPixels={290}
            type={InputType.Text}
            value={searchTerm}
            onBlur={this.handleFilterBlur}
            onChange={this.handleFilterChange}
            placeholder="Filter Labels..."
          />
          <Button
            text="Create Label"
            color={ComponentColor.Primary}
            icon={IconFont.Plus}
            onClick={this.handleShowOverlay}
          />
        </TabbedPageHeader>
        <FilterList<ILabel>
          list={labels}
          searchKeys={['name', 'description']}
          searchTerm={searchTerm}
        >
          {ls => (
            <LabelList
              labels={ls}
              emptyState={this.emptyState}
              onUpdateLabel={this.handleUpdateLabel}
              onDeleteLabel={this.handleDelete}
            />
          )}
        </FilterList>
        <CreateLabelOverlay
          visible={isOverlayVisible}
          onDismiss={this.handleDismissOverlay}
          onCreateLabel={this.handleCreateLabel}
          onNameValidation={this.handleNameValidation}
        />
      </>
    )
  }

  private handleShowOverlay = (): void => {
    this.setState({isOverlayVisible: OverlayState.Show})
  }

  private handleDismissOverlay = (): void => {
    this.setState({isOverlayVisible: OverlayState.Hide})
  }

  private handleFilterChange = (e: ChangeEvent<HTMLInputElement>): void => {
    this.setState({searchTerm: e.target.value})
  }

  private handleFilterBlur = (e: ChangeEvent<HTMLInputElement>): void => {
    this.setState({searchTerm: e.target.value})
  }

  private handleCreateLabel = (label: ILabel) => {
    this.props.createLabel(label.name, label.properties)
  }

  private handleUpdateLabel = (label: ILabel) => {
    this.props.updateLabel(label.id, label.properties)
  }

  private handleDelete = async (id: string) => {
    this.props.deleteLabel(id)
  }

  private handleNameValidation = (name: string): string | null => {
    const names = this.props.labels.map(label => label.name)

    return validateLabelUniqueness(names, name)
  }

  private get emptyState(): JSX.Element {
    const {searchTerm} = this.state

    if (searchTerm) {
      return (
        <EmptyState size={ComponentSize.Medium}>
          <EmptyState.Text text="No Labels match your search term" />
        </EmptyState>
      )
    }

    return (
      <EmptyState size={ComponentSize.Medium}>
        <EmptyState.Text
          text="Looks like you haven't created any Labels , why not create one?"
          highlightWords={['Labels']}
        />
        <Button
          text="Create Label"
          color={ComponentColor.Primary}
          icon={IconFont.Plus}
          onClick={this.handleShowOverlay}
        />
      </EmptyState>
    )
  }
}

const mstp = ({labels}: AppState): StateProps => {
  return {
    labels: labels.list,
  }
}

const mdtp: DispatchProps = {
  notify: notifyAction,
  createLabel: createLabel,
  updateLabel: updateLabel,
  deleteLabel: deleteLabel,
}

export default connect(
  mstp,
  mdtp
)(Labels)
