import {
  Toast,
  ToastClose,
  ToastDescription,
  ToastProvider,
  ToastTitle,
  ToastViewport
} from '@/components/shadcn/toast'
import { useToast } from '@/hooks/use-toast'
import { t } from '@/i18n'

export function Toaster() {
  const { toasts } = useToast()

  return (
    <ToastProvider>
      {toasts.map(({ id, title, description, action, ...props }) => (
        <Toast key={id} {...props}>
          <div className="grid gap-1">
            {title && <ToastTitle>{title}</ToastTitle>}
            {description && <ToastDescription>{description}</ToastDescription>}
          </div>
          {action}
          {/* An error toast stays until dismissed, so its close button is
              always shown rather than only on hover. */}
          <ToastClose
            aria-label={t('close')}
            data-test="toast-close"
            className="group-[.destructive]:opacity-100"
          />
        </Toast>
      ))}
      <ToastViewport />
    </ToastProvider>
  )
}
