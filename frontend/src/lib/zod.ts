import { config, z } from 'zod'

/**
 * zod for this app: import `z` from here, never from 'zod' (biome.json
 * enforces it).
 *
 * It turns zod's JIT off before any schema can exist. zod 4 compiles object
 * parsers with `new Function`, deciding per schema when it is built, and the
 * first one also probes whether eval is allowed. Both are Content-Security-
 * Policy violations: the panel's `script-src 'nonce-…' 'strict-dynamic'` has no
 * 'unsafe-eval' (and its Trusted Types policy admits no script), and it is
 * enforced: they would leave zod refused on every page. The schemas here are a
 * handful of form fields, so the JIT bought nothing.
 *
 * Because every schema module imports `z` through this one, the config runs
 * before any of them, whatever chunk the bundler puts them in. main.tsx also
 * imports it first, for anything zod builds on its own.
 */
config({ jitless: true })

export { z }
