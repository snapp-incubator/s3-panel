import { config } from 'zod'

/**
 * Keeps zod from compiling parsers with `new Function`.
 *
 * zod 4 probes once whether eval is allowed, when the first object schema is
 * built, and JIT-compiles object parsers if it is. The probe alone is a
 * Content-Security-Policy violation: report-only it fills the console, and
 * once the panel's CSP is enforced (no 'unsafe-eval') it would at best leave
 * zod on its slow path after a refusal. Turning the JIT off here, before any
 * schema exists, means nothing in the bundle evaluates strings, which is what
 * the CSP's `script-src 'nonce-…' 'strict-dynamic'` expects. The schemas this
 * panel parses are a handful of form fields, so the JIT bought nothing.
 *
 * Imported for its side effect, first in main.tsx: a schema built before this
 * runs has already made the decision.
 */
config({ jitless: true })
