import { useEffectOnce } from 'react-use'

import OIDCLogin from '@/components/oidc-login'
import { useTitle } from '@/components/providers/titleProvider'
import S3LoginForm from '@/components/s3-login-form'
import {
  Card,
  CardContent,
  CardDescription,
  CardTitle
} from '@/components/shadcn/card'
import usePanelSession from '@/hooks/usePanelSession'
import { t } from '@/i18n'

export default function S3BucketPage() {
  const { setTitle } = useTitle()
  // Which login to present is the server's decision, not a build-time flag, so
  // the same bundle serves both deployments.
  const isIAMMode = usePanelSession(state => state.authMode === 'iam')

  useEffectOnce(() => {
    setTitle(t('s3_pages_title'))
  })

  return (
    <div className="flex min-h-full min-w-full flex-col">
      <h2 className="text-3xl">{t('s3_bucket')}</h2>
      <Card className="m-auto min-h-[363px] w-[500px] p-6 pb-0">
        <CardTitle>{t('s3_bucket')}</CardTitle>
        <CardDescription className="mt-4">
          {isIAMMode
            ? t('sign_in_description')
            : t('s3_bucket_login_description')}
        </CardDescription>
        <CardContent className="pt-6">
          {isIAMMode ? <OIDCLogin /> : <S3LoginForm />}
        </CardContent>
      </Card>
    </div>
  )
}
