/**
 * The Trusted Types "default" policy: the one policy the panel's CSP lets exist
 * (`require-trusted-types-for 'script'; trusted-types default`, see
 * internal/api/security.go).
 *
 * The app itself writes no markup from strings. One dependency does: Radix
 * Select's Viewport renders a constant stylesheet through
 * `dangerouslySetInnerHTML`, which React assigns to `innerHTML` as a plain
 * string. Only a default policy sees such a string, so without one every select
 * on the page throws when it opens.
 *
 * The policy admits HTML only when it holds no `<`. Such a string parses to
 * text alone, whatever sink it reaches — no element, no attribute, so no
 * script — which covers that stylesheet without naming its exact text. Anything
 * with markup, and every script or script URL (no createScript or
 * createScriptURL), is refused and reported.
 *
 * It must run before anything renders: main.tsx imports it first.
 */

type DefaultPolicyRules = {
  createHTML: (input: string) => string | null
}

type TrustedTypesFactory = {
  createPolicy: (name: 'default', rules: DefaultPolicyRules) => unknown
}

const factory = (globalThis as { trustedTypes?: TrustedTypesFactory })
  .trustedTypes

// Browsers without Trusted Types enforce nothing, so there is nothing to admit.
factory?.createPolicy('default', {
  createHTML: input => (input.includes('<') ? null : input)
})
