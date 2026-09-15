import { RouterProvider } from '@tanstack/react-router'
import { useEffect } from 'react'

import { UploadProgressProvider } from './components/providers/uploadProgressContext'
import GlobalUploadProgress from './components/upload-progress'
import usePanelSession from './hooks/usePanelSession'
import useS3Credentials from './hooks/useS3Credentials'
import { router } from './router'
import { updateRegion } from './services/http/centralClient'

function App() {
  const { access_key, secret_key, region } = useS3Credentials()
  const { authMode, bootstrapError, bootstrap } = usePanelSession()

  // Read the panel's authentication mode before rendering any route: the route
  // guards ask which store holds "logged in", and until this resolves neither
  // answer is trustworthy.
  useEffect(() => {
    void bootstrap()
  }, [bootstrap])

  if (access_key && secret_key && region) {
    updateRegion(region, {
      access_key,
      secret_key
    })
  }

  // The panel could not be reached and has never been read, so which login to
  // present is unknown. Say so and offer a retry rather than guessing: guessing
  // used to mean falling back to the credential form, which on an iam
  // deployment asks a signed-in user for keys that do not exist.
  if (authMode === null && bootstrapError !== null) {
    return (
      <div className="flex h-screen flex-col items-center justify-center gap-4 text-center">
        <p className="text-lg font-medium">The panel is unavailable</p>
        <p className="text-muted-foreground max-w-md text-sm">
          {bootstrapError}
        </p>
        <button
          type="button"
          className="border-input hover:bg-accent rounded-md border px-4 py-2 text-sm"
          onClick={() => void bootstrap()}
        >
          Try again
        </button>
      </div>
    )
  }

  // A blank first paint is deliberate: rendering the credential form and then
  // swapping it for a sign-in button once the mode arrives is worse than a brief
  // pause, and worse still is bouncing a signed-in user to a login screen.
  if (authMode === null) return null

  return (
    <UploadProgressProvider>
      <GlobalUploadProgress />
      <RouterProvider router={router} />
    </UploadProgressProvider>
  )
}

export default App
