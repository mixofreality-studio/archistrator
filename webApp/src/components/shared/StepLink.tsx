/**
 * Inline router link to a Phase-1 (System Design) design-experience step — the
 * navigable-join affordance: a prose reference to another artifact (a trace id,
 * an owning component, a volatility hint) becomes a REAL link to that step.
 * Navigates the same surface SlimSpine drives — the deep-linkable
 * /project/$projectId/design/system/{-$stepSlug} route (slugForKind) — via a
 * real <a> (TanStack createLink over MUI Link, so `to`/`params` stay typed),
 * keyboard reachable, aria-label naming the target step. Outside a
 * RouterProvider (the MCP shell mounts views router-less) or without a
 * projectId route param in scope, the children render as plain text instead of
 * a dead link — router-dependent hooks live in an inner component that only
 * mounts once the context probe (useRouter warn:false) finds a router.
 */
import type { ReactNode } from 'react';
import { createLink, useParams, useRouter, type RegisteredRouter } from '@tanstack/react-router';
import MuiLink from '@mui/material/Link';
import Box from '@mui/material/Box';
import type { Theme } from '@mui/material/styles';
import type { SystemStyleObject } from '@mui/system';
import { METHOD_METADATA, slugForKind } from '../../contracts/methodMetadata';
import type { ArtifactKindFull } from '../../contracts/types';
import { useTokens } from '../../utilities/theme/ThemeContext';

/** MUI Link wired as a typed TanStack router link (the createLink recipe). */
const RouterMuiLink = createLink(MuiLink);

interface StepLinkProps {
  /** Target Phase-1 step (its artifact kind — slugForKind derives the URL). */
  kind: ArtifactKindFull;
  /** What this link IS (trace id / component name / hint) — composed with the
   *  target step's title into the aria-label so AT hears the destination. */
  label: string;
  children: ReactNode;
  /** Call-site chrome (font, color, chip borders); merged over the base style. */
  sx?: SystemStyleObject<Theme>;
  /** Optional search params carried on the link — e.g. { view } preselects a
   *  dynamic view on the Architecture step (the route's validated search). */
  search?: { view?: string };
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

  if (projectId === '') {
    return (
      <Box component="span" sx={sx}>
        {children}
      </Box>
    );
  }

  return (
    <RouterMuiLink
      aria-label={`${label} — open the ${METHOD_METADATA[kind].title} step`}
      data-testid={testId}
      params={{ projectId, stepSlug: slugForKind(kind) }}
      {...(search !== undefined ? { search } : {})}
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
      to="/project/$projectId/design/system/{-$stepSlug}"
      underline={underline}
    >
      {children}
    </RouterMuiLink>
  );
}
