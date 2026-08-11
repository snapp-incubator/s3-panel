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
  const { authMode, bootstrap } = usePanelSession()

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
