/**
 * Billing (`/project/$projectId/billing`) — a thin TOP-LEVEL surface. aiarch is a
 * PURE SERVICE VENDOR: it bills the USER for construction tokens + hosting,
 * usage-based, no subscription, no revenue share.
 *
 * The billing backend (billingManager + Stripe via billingGatewayAccess) is
 * HUMAN-GATED on Stripe provisioning and has NO read endpoint yet. So this screen
 * renders the INTENDED layout (current invoice · payment method · service pricing
 * · invoice history) as a CLEARLY-LABELLED, explicitly-pending placeholder — NO
 * fake data, NO fetch to a non-existent endpoint that would 404. It is wired into
 * nav so the surface exists, gated as "billing backend not yet provisioned".
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Chip from '@mui/material/Chip';
import Button from '@mui/material/Button';
import ReceiptLongOutlinedIcon from '@mui/icons-material/ReceiptLongOutlined';
import CreditCardOutlinedIcon from '@mui/icons-material/CreditCardOutlined';
import { getRouteApi, useNavigate } from '@tanstack/react-router';
import { AppShell } from '../components/AppShell';
import { useTokens } from '../theme/ThemeContext';
import type { Tokens } from '../theme/themes';
import { UI_IDENTIFIERS } from '../constants/UIIdentifiers';

const routeApi = getRouteApi('/project/$projectId/billing');

export function BillingScreen(): ReactNode {
  const { projectId } = routeApi.useParams();
  return (
    <AppShell projectId={projectId}>
      <BillingBody projectId={projectId} />
    </AppShell>
  );
}

function BillingBody({ projectId }: { projectId: string }): ReactNode {
  const t = useTokens();
  const navigate = useNavigate();

  return (
    <Box
      data-testid={UI_IDENTIFIERS.Billing.ROOT}
      sx={{ maxWidth: 1080, mx: 'auto', px: { xs: 2, md: 4 }, py: 4 }}
    >
      {/* header */}
      <Box sx={{ display: 'flex', alignItems: 'flex-end', gap: 2, mb: 1, flexWrap: 'wrap' }}>
        <Box>
          <Typography sx={{ color: t.muted }} variant="overline">aiarch service invoices · usage-based</Typography>
          <Typography sx={{ color: t.ink }} variant="h3">Billing</Typography>
        </Box>
        <Box sx={{ flexGrow: 1 }} />
        <Chip label="BACKEND PENDING" size="small" sx={{ bgcolor: t.awaitingBg, color: t.awaitingFg, fontFamily: t.mono, fontWeight: 700 }} />
        <Button
          data-testid={UI_IDENTIFIERS.Billing.HOME_LINK}
          sx={{ color: t.muted }}
          variant="text"
          onClick={() => void navigate({ to: '/project/$projectId/home', params: { projectId } })}
        >
          ← Home base
        </Button>
      </Box>

      {/* model banner */}
      <Box sx={{ mb: 3, p: 1.25, bgcolor: t.committedBg, border: `1.5px solid ${t.line}`, borderRadius: t.radius / 8 + 0.5 }}>
        <Typography sx={{ fontFamily: t.mono, fontSize: 11.5, color: t.committedFg, lineHeight: 1.5 }}>
          aiarch is a <b>pure service vendor</b>: it bills <b>you</b> for <b>construction tokens</b> + <b>hosting</b>, usage-based, no subscription. No
          revenue share — you own your code and are merchant of record for your own app.
        </Typography>
      </Box>

      {/* the explicit pending state */}
      <Paper
        data-testid={UI_IDENTIFIERS.Billing.PENDING_STATE}
        sx={{ p: 4, textAlign: 'center', borderStyle: 'dashed', mb: 3 }}
      >
        <ReceiptLongOutlinedIcon sx={{ fontSize: 32, color: t.muted, opacity: 0.6 }} />
        <Typography sx={{ fontFamily: t.display, fontWeight: 700, fontSize: 18, color: t.ink, mt: 1 }}>
          Billing backend not yet provisioned
        </Typography>
        <Box sx={{ maxWidth: 560, mx: 'auto' }}>
          <Typography sx={{ fontFamily: t.body, fontSize: 13, color: t.muted, mt: 0.5, lineHeight: 1.5 }}>
            The billing manager (Stripe via billingGatewayAccess) is human-gated on Stripe provisioning and has no read endpoint yet. No invoices are
            metered or charged here. The intended layout is previewed below so the surface is in place; it activates once billing is provisioned.
          </Typography>
        </Box>
      </Paper>

      {/* the INTENDED layout — clearly-labelled placeholders, NO data, NO fetch */}
      <Box aria-hidden sx={{ display: 'flex', flexDirection: 'column', gap: 1.5, opacity: 0.55, pointerEvents: 'none', filter: 'grayscale(0.4)' }}>
        <Placeholder icon={<ReceiptLongOutlinedIcon sx={{ fontSize: 16 }} />} sub="construction tokens (to date) · hosting (to date) · invoice total" t={t} title="Current invoice — amount owed to aiarch" />
        <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', md: '1fr 1fr' }, gap: 1.5 }}>
          <Placeholder icon={<CreditCardOutlinedIcon sx={{ fontSize: 16 }} />} sub="card on file · managed via Stripe" t={t} title="Payment method" />
          <Placeholder sub="usage-based price book · tokens + hosting meters" t={t} title="Service pricing" />
        </Box>
        <Placeholder sub="prior service invoices · period · tokens · hosting · total · status" t={t} title="Invoice history" />
      </Box>
    </Box>
  );
}

function Placeholder({ t, icon, title, sub }: { t: Tokens; icon?: ReactNode; title: string; sub: string }): ReactNode {
  return (
    <Paper sx={{ p: 0, overflow: 'hidden' }}>
      <Box sx={{ px: 2, py: 1.1, bgcolor: t.paperAlt, borderBottom: `1.5px solid ${t.line}`, display: 'flex', alignItems: 'center', gap: 1 }}>
        {icon !== undefined && <Box sx={{ color: t.muted, display: 'flex' }}>{icon}</Box>}
        <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 11, letterSpacing: '0.06em', color: t.ink }}>{title.toUpperCase()}</Typography>
        <Box sx={{ flexGrow: 1 }} />
        <Chip label="pending" size="small" sx={{ height: 18, fontSize: 8.5, fontFamily: t.mono, color: t.muted }} variant="outlined" />
      </Box>
      <Box sx={{ px: 2, py: 1.75 }}>
        <Box sx={{ height: 26, width: '40%', bgcolor: t.paperAlt, borderRadius: t.radius / 8 + 0.5, mb: 0.75 }} />
        <Typography sx={{ fontFamily: t.mono, fontSize: 10, color: t.muted }}>{sub}</Typography>
      </Box>
    </Paper>
  );
}
