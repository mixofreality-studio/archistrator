/**
 * Billing (`/project/$projectId/billing`) — a thin TOP-LEVEL surface. archistrator
 * (aiarch) is a PURE SERVICE VENDOR: it bills the USER for construction tokens + hosting,
 * usage-based, no subscription, no revenue share.
 *
 * Two surfaces are live: payment onboarding (BillingOnboarding —
 * billingManager.OnboardPaymentIntegration + RegisterCustomer, use case
 * onboard-a-new-customer) and the billing-period lookup (BillingCycleLookup —
 * billingManager.GetBillingCycle, use case view-a-billing-cycle). Everything else
 * the billing backend (billingManager + Stripe via merchantGatewayAccess) would show
 * has NO read endpoint yet, so the rest of the screen renders the INTENDED layout
 * (current invoice · payment method · service pricing · invoice history) as a
 * CLEARLY-LABELLED, explicitly-pending placeholder — NO fake data, NO fetch to a
 * non-existent endpoint that would 404.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import Chip from '@mui/material/Chip';
import ReceiptLongOutlinedIcon from '@mui/icons-material/ReceiptLongOutlined';
import CreditCardOutlinedIcon from '@mui/icons-material/CreditCardOutlined';
import { getRouteApi } from '@tanstack/react-router';
import { AppShell } from '../components/AppShell';
import { BillingCycleLookupContainer } from '../containers/BillingCycleLookupContainer';
import { BillingOnboardingContainer } from '../containers/BillingOnboardingContainer';
import { useTokens } from '../utilities/theme/ThemeContext';
import type { Tokens } from '../utilities/theme/themes';
import { UI_IDENTIFIERS } from '../utilities/constants/UIIdentifiers';

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

  return (
    <Box
      data-testid={UI_IDENTIFIERS.Billing.ROOT}
      sx={{ maxWidth: 1080, mx: 'auto', px: { xs: 2, md: 4 }, py: 4 }}
    >
      {/* header */}
      <Box sx={{ mb: 1 }}>
        <Typography sx={{ color: t.muted }} variant="overline">
          What archistrator charges you · usage-based
        </Typography>
        <Typography component="h1" sx={{ color: t.ink }} variant="h3">
          Billing
        </Typography>
      </Box>

      {/* model banner */}
      <Box
        sx={{
          mb: 3,
          p: 1.5,
          bgcolor: t.committedBg,
          border: `1.5px solid ${t.line}`,
          borderRadius: t.radius / 8 + 0.5,
        }}
      >
        <Typography sx={{ fontSize: 13.5, color: t.committedFg, lineHeight: 1.55 }}>
          archistrator bills <b>you</b> only for what you use: the AI work that designs and builds
          your system, and <b>hosting</b> once it runs. No subscription and no revenue share — you
          own your code and are the merchant of record for your own app&rsquo;s customers.
        </Typography>
      </Box>

      {/* payment onboarding: open it for a deployed app, then register the customer */}
      <BillingOnboardingContainer projectId={projectId} />

      {/* the one live read: a billing period's recorded settlement */}
      <BillingCycleLookupContainer />

      {/* the explicit pending state */}
      <Paper
        data-testid={UI_IDENTIFIERS.Billing.PENDING_STATE}
        sx={{ p: 4, textAlign: 'center', borderStyle: 'dashed', mb: 3 }}
      >
        <ReceiptLongOutlinedIcon aria-hidden sx={{ fontSize: 32, color: t.muted, opacity: 0.6 }} />
        <Typography
          component="h2"
          sx={{ fontFamily: t.display, fontWeight: 700, fontSize: 18, color: t.ink, mt: 1 }}
        >
          Invoices, payment method and pricing — coming soon
        </Typography>
        <Box sx={{ maxWidth: 560, mx: 'auto' }}>
          <Typography
            sx={{ fontFamily: t.body, fontSize: 13, color: t.muted, mt: 0.5, lineHeight: 1.5 }}
          >
            Today you can set up payments and look up one billing period above. Your current
            invoice, the card on file, the price list and past invoices will appear here; the
            outline below shows where each will go.
          </Typography>
        </Box>
      </Paper>

      {/* the INTENDED layout — clearly-labelled placeholders, NO data, NO fetch.
          Hidden from assistive tech: the card above already says what is coming. */}
      <Box
        aria-hidden
        sx={{
          display: 'flex',
          flexDirection: 'column',
          gap: 1.5,
          pointerEvents: 'none',
        }}
      >
        <Placeholder
          icon={<ReceiptLongOutlinedIcon sx={{ fontSize: 16 }} />}
          sub="AI build work to date · hosting to date · total"
          t={t}
          title="Current invoice — what you owe so far"
        />
        <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', md: '1fr 1fr' }, gap: 1.5 }}>
          <Placeholder
            icon={<CreditCardOutlinedIcon sx={{ fontSize: 16 }} />}
            sub="card on file · managed via Stripe"
            t={t}
            title="Payment method"
          />
          <Placeholder
            sub="what each unit of AI work and hosting costs"
            t={t}
            title="Service pricing"
          />
        </Box>
        <Placeholder
          sub="past invoices · month · AI work · hosting · total · paid?"
          t={t}
          title="Invoice history"
        />
      </Box>
    </Box>
  );
}

function Placeholder({
  t,
  icon,
  title,
  sub,
}: {
  t: Tokens;
  icon?: ReactNode;
  title: string;
  sub: string;
}): ReactNode {
  return (
    <Paper sx={{ p: 0, overflow: 'hidden' }}>
      <Box
        sx={{
          px: 2,
          py: 1.1,
          bgcolor: t.paperAlt,
          borderBottom: `1.5px solid ${t.line}`,
          display: 'flex',
          alignItems: 'center',
          gap: 1,
        }}
      >
        {icon !== undefined && <Box sx={{ color: t.muted, display: 'flex' }}>{icon}</Box>}
        <Typography sx={{ fontWeight: 700, fontSize: 13, color: t.ink }}>{title}</Typography>
        <Box sx={{ flexGrow: 1 }} />
        <Chip
          label="Coming soon"
          size="small"
          sx={{ height: 20, fontSize: 11, color: t.muted, borderColor: t.line }}
          variant="outlined"
        />
      </Box>
      <Box sx={{ px: 2, py: 1.75 }}>
        <Box
          sx={{
            height: 26,
            width: '40%',
            bgcolor: t.paperAlt,
            borderRadius: t.radius / 8 + 0.5,
            mb: 0.75,
          }}
        />
        <Typography sx={{ fontSize: 12, color: t.muted }}>{sub}</Typography>
      </Box>
    </Paper>
  );
}
