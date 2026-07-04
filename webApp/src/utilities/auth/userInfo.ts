/**
 * The signed-in user, as returned by GET /api/userinfo. The keys mirror the
 * server's framework-go `security.UserInfoResponse`, which maps the validated
 * principal's OIDC claims. The Envoy edge authenticates the browser and forwards
 * the access token to the server; the SPA only reads this shape — it never runs
 * its own OIDC flow (GTD parity).
 */
export interface UserInfoOrganization {
  readonly id: string;
  readonly name: string;
}

export interface UserInfo {
  readonly kind: string;
  readonly sub: string;
  readonly preferred_username?: string;
  readonly email?: string;
  readonly name?: string;
  readonly roles?: readonly string[];
  readonly organizations?: readonly UserInfoOrganization[];
}

/** Best human-facing label for the signed-in user. */
export function userLabel(user: UserInfo): string {
  return user.preferred_username ?? user.name ?? user.email ?? user.sub;
}
