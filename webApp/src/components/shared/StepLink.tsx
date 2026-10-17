/**
 * Inline router link from one design artifact to another — the navigable-join
 * affordance: a prose reference to another artifact (a trace id, an owning
 * component, a volatility hint) becomes a REAL link to the review task that
 * judges that artifact in the Activity Experience
 * (`ACTIVITY_PATH?task=<review>`, e.g. the Glossary → requirements /
 * glossaryReview, the System → architecture / architectureReview). The
 * (activity, task) pair is DERIVED from the generated lifecycles by
 * `reviewRefFor`, never hand-listed; a kind no lifecycle task produces (the
 * Required Behaviors ride the glossary dispatch) has no page of its own and
 * renders as plain text rather than a dead link. Rendered as a real <a>
 * (TanStack createLink over MUI Link, so `to`/`params` stay typed), keyboard
 * reachable, aria-label naming the target. Outside a RouterProvider (the MCP
 * shell mounts views router-less) or without a projectId route param in
 * scope, the children render as plain text too — router-dependent hooks live
 * in an inner component that only mounts once the context probe (useRouter
 * warn:false) finds a router.
 */
import type { ReactNode } from 'react';
import { createLink, useParams, useRouter, type RegisteredRouter } from '@tanstack/react-router';
import MuiLink from '@mui/material/Link';
import Box from '@mui/material/Box';
import type { Theme } from '@mui/material/styles';
import type { SystemStyleObject } from '@mui/system';
import { METHOD_METADATA } from '../../contracts/methodMetadata';
import { ACTIVITY_PATH, activitySearch } from '../../contracts/routePaths';
import type { ArtifactKindFull } from '../../contracts/types';
import { useTokens } from '../../utilities/theme/ThemeContext';
import { reviewRefFor } from '../activity/designTaskRef';

/** MUI Link wired as a typed TanStack router link (the createLink recipe). */
const RouterMuiLink = createLink(MuiLink);

interface StepLinkProps {
  /** Target artifact kind — reviewRefFor derives the (activity, review task). */
  kind: ArtifactKindFull;
  /** What this link IS (trace id / component name / hint) — composed with the
   *  target artifact's title into the aria-label so AT hears the destination. */
  label: string;
  children: ReactNode;
  /** Call-site chrome (font, color, chip borders); merged over the base style. */
  sx?: SystemStyleObject<Theme>;
  /** Optional search params carried on the link — e.g. { view } preselects a
   *  dynamic view on the Architecture review, and { view, step } additionally
   *  lands on that view's 1-based step (the activity route's validated search,
   *  `activitySearch`). */
  search?: { view?: string; step?: number };
  testId?: string;
  /** 'always' for links inside prose (non-color distinction); 'none' for
   *  chip-shaped links whose border already signals interactivity. */
  underline?: 'always' | 'hover' | 'none';
}

export function StepLink(props: StepLinkProps): ReactNode {
  // Probe-only context read: unlike useParams/useRouterState, useRouter with
  // warn:false RETURNS UNDEFINED outside a RouterProvider instead of crashing
  // (its declared type ignores that case, hence the widening cast).
  const router = useRouter({ warn: false }) as RegisteredRouter | undefined;
  if (router === undefined) {
    return (
      <Box component="span" sx={props.sx}>
        {props.children}
      </Box>
    );
  }
  return <RoutedStepLink {...props} />;
}

function RoutedStepLink({
  kind,
  label,
  children,
  sx,
  search,
  testId,
  underline = 'always',
}: StepLinkProps): ReactNode {
  const t = useTokens();
  const params = useParams({ strict: false });
  const projectId = typeof params.projectId === 'string' ? params.projectId : '';
  const target = reviewRefFor(kind);

  if (projectId === '' || target === undefined) {
    return (
      <Box component="span" sx={sx}>
        {children}
      </Box>
    );
  }

  return (
    <RouterMuiLink
      aria-label={`${label} — open the ${METHOD_METADATA[kind].title} review`}
      data-testid={testId}
      params={{ projectId, activityId: target.activityId }}
      search={activitySearch(target.taskId, undefined, search?.view, search?.step)}
      sx={[
        {
          color: 'inherit',
          textDecorationColor: 'currentColor',
          borderRadius: 0.5,
          outline: 'none',
          '&:focus-visible': { outline: `2px solid ${t.accent}`, outlineOffset: 1 },
        },
        ...(sx !== undefined ? [sx] : []),
      ]}
      to={ACTIVITY_PATH}
      underline={underline}
    >
      {children}
    </RouterMuiLink>
  );
}
