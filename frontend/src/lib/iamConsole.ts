/**
 * Where a bucket's access is managed in the IAM console: its own resource page.
 *
 * Null unless the deployment says where the console is (`iam_console_url`,
 * empty in s3 mode and when not configured) and IAM reported the bucket's
 * resource ID — without either there is nothing to link to.
 */
export const manageAccessHref = (
  iamConsoleUrl: string,
  resourceId?: string
): string | null =>
  iamConsoleUrl && resourceId
    ? `${iamConsoleUrl}/resources/${encodeURIComponent(resourceId)}`
    : null
