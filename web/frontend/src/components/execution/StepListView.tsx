import { Clock } from 'lucide-react'
import type { Step } from '@/types/execution'
import { calculateDuration, getStatusColor, getStatusIcon } from './stepUtils'

export interface StepListViewProps {
  steps: Step[]
  selectedStep: Step | null
  onSelectStep: (step: Step) => void
}

// 列表视图：递归渲染步骤树（含空态）
export function StepListView({ steps, selectedStep, onSelectStep }: StepListViewProps) {
  const renderStepTree = (stepList: Step[], indent = 0) => {
    return stepList.map((step) => (
      <div key={step.id}>
        <div
          className={`px-4 py-3 cursor-pointer transition-colors border-l-4 ${getStatusColor(step.status)} ${
            selectedStep?.id === step.id ? 'ring-2 ring-blue-500 ring-inset' : ''
          }`}
          style={{ paddingLeft: `${indent + 16}px` }}
          onClick={() => onSelectStep(step)}
        >
          <div className="flex items-center gap-3">
            {getStatusIcon(step.status)}
            <div className="flex-1 min-w-0">
              <div className="flex items-center justify-between">
                <p className="font-medium text-gray-900 dark:text-white truncate">
                  {step.name}
                </p>
                <span className="text-xs text-gray-500 dark:text-gray-400">
                  {calculateDuration(step.startTime, step.endTime)}
                </span>
              </div>
              {step.action && (
                <p className="text-sm text-gray-500 dark:text-gray-400 truncate">
                  {step.action}
                </p>
              )}
            </div>
          </div>
        </div>
        {step.children && step.children.length > 0 && renderStepTree(step.children, indent + 20)}
      </div>
    ))
  }

  return (
    <div className="h-full overflow-y-auto">
      <div className="divide-y dark:divide-gray-700">
        {steps.length === 0 ? (
          <div className="px-4 py-8 text-center text-gray-500 dark:text-gray-400">
            <Clock className="w-8 h-8 mx-auto mb-2 opacity-50" />
            <p>No steps yet</p>
          </div>
        ) : (
          renderStepTree(steps)
        )}
      </div>
    </div>
  )
}
