// theme.jsx — Trine design tokens, computed from tweak state

function makeTokens(tk) {
  // tk: { theme: 'midnight'|'onyx'|'graphite', accent: '#5EEAD4'|... }
  const accent = tk.accent || '#5EEAD4';
  const themes = {
    midnight: {
      bg:       '#0A0F1C',
      bgGrad:   'radial-gradient(120% 70% at 50% -10%, #14224A 0%, #0A0F1C 55%)',
      surface:  '#121829',
      surface2: '#1A2236',
      hairline: 'rgba(255,255,255,0.07)',
      text:     '#F5F7FA',
      text2:    'rgba(245,247,250,0.62)',
      text3:    'rgba(245,247,250,0.38)',
      danger:   '#FF6E6E',
      warn:     '#F7C25A',
      ok:       accent,
    },
    onyx: {
      bg:       '#06070A',
      bgGrad:   'radial-gradient(120% 60% at 50% -10%, #1A1C24 0%, #06070A 55%)',
      surface:  '#0F1116',
      surface2: '#171A22',
      hairline: 'rgba(255,255,255,0.06)',
      text:     '#FFFFFF',
      text2:    'rgba(255,255,255,0.6)',
      text3:    'rgba(255,255,255,0.36)',
      danger:   '#FF6E6E',
      warn:     '#F7C25A',
      ok:       accent,
    },
    graphite: {
      bg:       '#13141A',
      bgGrad:   'radial-gradient(120% 60% at 50% -10%, #262833 0%, #13141A 55%)',
      surface:  '#1B1D26',
      surface2: '#24262F',
      hairline: 'rgba(255,255,255,0.08)',
      text:     '#F2F3F5',
      text2:    'rgba(242,243,245,0.62)',
      text3:    'rgba(242,243,245,0.40)',
      danger:   '#FF6E6E',
      warn:     '#F7C25A',
      ok:       accent,
    },
  };
  return { ...themes[tk.theme || 'midnight'], accent };
}

// reusable atoms
function Card({ tk, children, style, ...rest }) {
  return <div {...rest} style={{
    background: tk.surface, borderRadius: 20,
    border: `0.5px solid ${tk.hairline}`,
    ...style,
  }}>{children}</div>;
}

function PrimaryBtn({ tk, children, onClick, disabled, style }) {
  return <button onClick={onClick} disabled={disabled} style={{
    width:'100%', height:54, borderRadius:16,
    background: disabled ? tk.surface2 : tk.accent,
    color: disabled ? tk.text3 : '#06070A',
    border:'none', fontSize:16, fontWeight:600, letterSpacing:0.2,
    fontFamily:'inherit', cursor:'pointer',
    boxShadow: disabled ? 'none' : `0 8px 28px ${tk.accent}33, inset 0 1px 0 rgba(255,255,255,0.25)`,
    ...style,
  }}>{children}</button>;
}

function GhostBtn({ tk, children, onClick, style }) {
  return <button onClick={onClick} style={{
    height:54, borderRadius:16, padding:'0 20px',
    background: tk.surface, color: tk.text,
    border:`0.5px solid ${tk.hairline}`,
    fontSize:16, fontWeight:500, letterSpacing:0.2,
    fontFamily:'inherit', cursor:'pointer',
    ...style,
  }}>{children}</button>;
}

function TopBar({ tk, title, onBack, right, big }) {
  return (
    <div style={{ padding:'60px 16px 8px', display:'flex', alignItems:'center', justifyContent:'space-between', minHeight: big ? 'auto' : 56 }}>
      {onBack ? (
        <button onClick={onBack} style={{
          width:36, height:36, borderRadius:18, background:tk.surface,
          border:`0.5px solid ${tk.hairline}`, color:tk.text,
          display:'flex', alignItems:'center', justifyContent:'center', cursor:'pointer',
        }}>{Icon.chevronL(18, tk.text)}</button>
      ) : <div style={{width:36}}/>}
      <div style={{ flex:1, textAlign:'center', color:tk.text, fontWeight:600, fontSize:16, letterSpacing:0.2 }}>{title}</div>
      <div style={{ width:36, display:'flex', justifyContent:'flex-end' }}>{right}</div>
    </div>
  );
}

function TrineMark({ size=26, color, glow }) {
  // triangle with three nodes
  return (
    <svg width={size} height={size} viewBox="0 0 32 32" fill="none">
      <polygon points="16,4 28,26 4,26" stroke={color} strokeWidth="1.5" fill={glow ? color : 'none'} fillOpacity={glow ? 0.12 : 0} strokeLinejoin="round"/>
      <circle cx="16" cy="4" r="2.5" fill={color}/>
      <circle cx="28" cy="26" r="2.5" fill={color}/>
      <circle cx="4"  cy="26" r="2.5" fill={color}/>
    </svg>
  );
}

Object.assign(window, { makeTokens, Card, PrimaryBtn, GhostBtn, TopBar, TrineMark });
