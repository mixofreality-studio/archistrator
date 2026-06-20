/**
 * Taskbar control to flip between the five candidate design languages. A palette
 * icon opens a menu of swatch previews; selecting one persists via ThemeContext.
 */
import { useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Menu from '@mui/material/Menu';
import MenuItem from '@mui/material/MenuItem';
import Typography from '@mui/material/Typography';
import IconButton from '@mui/material/IconButton';
import Tooltip from '@mui/material/Tooltip';
import PaletteOutlinedIcon from '@mui/icons-material/PaletteOutlined';
import CheckIcon from '@mui/icons-material/Check';
import { useThemeSwitch } from '../theme/ThemeContext';
import { THEME_ORDER, TOKENS } from '../theme/themes';
import { UI_IDENTIFIERS } from '../constants/UIIdentifiers';

export function ThemeSwitcher(): ReactNode {
  const { themeKey, setThemeKey, tokens } = useThemeSwitch();
  const [anchorEl, setAnchorEl] = useState<HTMLElement | null>(null);

  return (
    <>
      <Tooltip title={`Theme · ${tokens.name}`}>
        <IconButton
          data-testid={UI_IDENTIFIERS.Shell.THEME_SWITCHER}
          size="small"
          sx={{
            color: tokens.ink,
            border: `1.5px solid ${tokens.line}`,
            borderRadius: tokens.radius / 8 + 0.5,
          }}
          onClick={(e) => {
            setAnchorEl(e.currentTarget);
          }}
        >
          <PaletteOutlinedIcon sx={{ fontSize: 18 }} />
        </IconButton>
      </Tooltip>
      <Menu
        anchorEl={anchorEl}
        open={anchorEl !== null}
        onClose={() => {
          setAnchorEl(null);
        }}
      >
        {THEME_ORDER.map((k) => {
          const tk = TOKENS[k];
          return (
            <MenuItem
              data-testid={UI_IDENTIFIERS.Shell.themeOption(k)}
              key={k}
              selected={k === themeKey}
              sx={{ gap: 1.5, py: 1 }}
              onClick={() => {
                setThemeKey(k);
                setAnchorEl(null);
              }}
            >
              <Box sx={{ display: 'flex', gap: 0.4 }}>
                {[tk.bg, tk.paper, tk.accent, tk.accent2].map((c, i) => (
                  <Box
                    key={i}
                    sx={{ width: 14, height: 14, bgcolor: c, border: `1px solid ${tk.line}` }}
                  />
                ))}
              </Box>
              <Box sx={{ flexGrow: 1 }}>
                <Typography sx={{ fontWeight: 600, fontSize: 14, fontFamily: tokens.body }}>
                  {tk.name}
                </Typography>
                <Typography
                  sx={{ fontFamily: tokens.mono, fontSize: 10.5, color: 'text.secondary' }}
                >
                  {tk.tag}
                </Typography>
              </Box>
              {k === themeKey ? <CheckIcon sx={{ fontSize: 16 }} /> : null}
            </MenuItem>
          );
        })}
      </Menu>
    </>
  );
}
