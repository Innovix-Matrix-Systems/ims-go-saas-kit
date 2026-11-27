/**
 * Product switches for kit features a product built on the kit may not use.
 *
 * Branches: set `branchesEnabled` to false when every organization has exactly
 * one branch, its default one. Give every plan `branches.max` = 1 so the API
 * refuses a second branch; the app then hides the branch page, its dashboard
 * shortcut, the branch count and the branch limit on plans and usage. The
 * branch switcher and the member branch picker already hide themselves with a
 * single branch.
 */
export const branchesEnabled = true
