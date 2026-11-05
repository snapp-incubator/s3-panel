/**
 * Types for the two authentication modes the panel supports.
 *
 * - `s3`  — the user supplies their own S3 credentials, and the gateway is the
 *           only authorization layer. This is the default and is unchanged.
 * - `iam` — the user signs in with OIDC, and a control endpoint answers which
 *           buckets they may use and with what permission.
 */
export type TAuthMode = 's3' | 'iam'

export interface IPanelConfigResponse {
  auth_mode: TAuthMode
  /** Where to send the browser to start an OIDC login. Empty in `s3` mode. */
  login_url?: string
  region?: string
  /**
   * Every mutating operation is refused server-side. The UI hides the controls
   * rather than offering actions that will 403.
   */
  read_only?: boolean
  /**
   * Browser-facing address of the IAM console, in `iam` mode, when configured.
   * Bucket cards link to each bucket's page there to manage access.
   */
  iam_console_url?: string
  /**
   * Set only when the panel is one app of a unified console: the console's
   * shared header sits above it, and these are the console's pages.
   */
  panel?: IPanelConsole
}

export interface IPanelConsole {
  app_key: string
  home_url: string
  signout_url: string
}

export interface IPanelSessionResponse {
  authenticated: boolean
  /**
   * Not signed in because the person just signed out on purpose; reported once.
   * Lets the console's silent sign-in run again without waiting out its guard.
   */
  signed_out?: boolean
  subject?: string
  email?: string
  name?: string
  groups?: string[]
  /** Member of a configured admin group, so the admin view is offered. */
  is_admin?: boolean
  /** Debug user being acted as (IAM proposal 008); empty when not acting. */
  act_as?: string
  /** Whether the real signed-in user is an admin, even while acting. */
  real_is_admin?: boolean
}

export interface IActAsState {
  available: boolean
  acting?: string
  candidates: {
    email: string
    username: string
    first_name?: string
    last_name?: string
  }[]
}

/** One bucket in `iam` mode, with the caller's permissions attached. */
export interface IIAMBucket {
  bucket: string
  display_name: string
  /** How the gateway addresses this bucket, which for a tenanted bucket differs from `bucket`. */
  s3_name?: string
  /** The bucket's IAM resource ID, which addresses its page in the IAM console. */
  resource_id?: string
  tenant?: string
  region?: string
  permissions: string[]
  /** The team the access was inherited from, when it was not granted directly. */
  granted_via?: string
  can_read: boolean
  can_write: boolean
  can_delete: boolean
}

export interface IIAMBucketListResponse {
  items: IIAMBucket[]
  /** Every region represented in the list. One call spans all of them. */
  regions: string[]
  total_buckets: number
  total_pages: number
}

export interface IIAMBucketDetailResponse extends IIAMBucket {
  num_objects: number
  size_bytes: number
  /** -1 means no quota is enforced, matching the gateway's own convention. */
  quota_bytes: number
  quota_objects: number
  policy: unknown | null
  policy_present: boolean
  /** Reading a policy takes more than read access; the rest of the page still renders. */
  policy_denied: boolean
}
