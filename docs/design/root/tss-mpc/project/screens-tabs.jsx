// screens-tabs.jsx — Inbox / Groups / Audit / Settings tabs (B-side signer)

// ─────────────────────────────────────────────────────────────
// Bottom tab bar
// ─────────────────────────────────────────────────────────────
function BottomTabs({ tk, active, onChange, badge }) {
  const tabs = [
    { id: 'inbox',     label: '请求', ico:Icon.inbox },
    { id: 'groups',    label: '钱包', ico:Icon.group },
    { id: 'audit',     label: '审计', ico:Icon.list },
    { id: 'settings',  label: '设置', ico:Icon.cog },
  ];
  return (
    <div style={{
      position:'absolute', left:0, right:0, bottom:0, zIndex:40,
      paddingBottom:24, paddingTop:8,
      background: `linear-gradient(180deg, transparent 0%, ${tk.bg} 50%)`,
    }}>
      <div style={{
        margin:'0 14px', borderRadius:24, padding:'8px 6px',
        background: 'rgba(255,255,255,0.04)',
        backdropFilter:'blur(20px) saturate(160%)',
        WebkitBackdropFilter:'blur(20px) saturate(160%)',
        border:`0.5px solid ${tk.hairline}`,
        display:'flex',
      }}>
        {tabs.map(t => {
          const on = active === t.id;
          const showBadge = t.id === 'inbox' && badge > 0;
          return (
            <button key={t.id} onClick={()=>onChange(t.id)} style={{
              flex:1, display:'flex', flexDirection:'column', alignItems:'center', gap:3,
              padding:'8px 4px', background:'transparent', border:'none', cursor:'pointer',
              color: on ? tk.accent : tk.text2,
              fontFamily:'inherit', fontSize:10.5, fontWeight: on ? 600 : 500, letterSpacing:0.4,
              position:'relative',
            }}>
              <div style={{ position:'relative' }}>
                {t.ico(20, on ? tk.accent : tk.text2)}
                {showBadge && (
                  <div style={{
                    position:'absolute', top:-3, right:-7,
                    minWidth:15, height:15, padding:'0 4px', borderRadius:8,
                    background:tk.warn, color:'#0A0F1C',
                    fontSize:9, fontWeight:700, letterSpacing:0,
                    display:'flex', alignItems:'center', justifyContent:'center',
                    border:`1.5px solid ${tk.bg}`,
                  }}>{badge}</div>
                )}
              </div>
              <span>{t.label}</span>
            </button>
          );
        })}
      </div>
    </div>
  );
}

// ─────────────────────────────────────────────────────────────
// Shared status pill — coord + relay
// ─────────────────────────────────────────────────────────────
function CoordPill({ tk }) {
  return (
    <div style={{
      display:'flex', alignItems:'center', gap:6,
      padding:'5px 10px 5px 8px', borderRadius:99,
      background:tk.surface, border:`0.5px solid ${tk.hairline}`,
    }}>
      <span style={{
        width:7, height:7, borderRadius:4, background:tk.accent,
        boxShadow:`0 0 6px ${tk.accent}`,
        animation:'trinePulse 1.8s ease-in-out infinite',
      }}/>
      <span style={{ color:tk.text2, fontSize:11, fontWeight:500, letterSpacing:0.3 }}>
        coord · {COORD.latencyMs}ms
      </span>
    </div>
  );
}

// ─────────────────────────────────────────────────────────────
// Chain badge
// ─────────────────────────────────────────────────────────────
function ChainBadge({ tk, chain, label, small }) {
  const colors = {
    'eip155:1':     '#627EEA',
    'eip155:42161': '#28A0F0',
    'tron':         '#EE3F47',
  };
  const c = colors[chain] || tk.text2;
  return (
    <div style={{
      display:'inline-flex', alignItems:'center', gap:5,
      padding: small ? '2px 7px' : '3px 9px', borderRadius:6,
      background:`${c}1f`, color:c,
      fontSize: small ? 10 : 11, fontWeight:600, letterSpacing:0.3,
    }}>
      <span style={{ width:5, height:5, borderRadius:3, background:c }}/>
      {label}
    </div>
  );
}

// ─────────────────────────────────────────────────────────────
// Inbox tab — pending signing requests (B3)
// ─────────────────────────────────────────────────────────────
function fmtExpiry(s) {
  if (s <= 0) return '已过期';
  if (s < 60) return s + 's';
  const m = Math.floor(s/60);
  const sec = s%60;
  return m + 'm ' + (sec < 10 ? '0' : '') + sec + 's';
}

function InboxTab({ tk, nav, pending }) {
  return (
    <div style={{ paddingBottom:120 }}>
      {/* App-bar */}
      <div style={{ padding:'58px 18px 8px', display:'flex', alignItems:'center', justifyContent:'space-between' }}>
        <div style={{ display:'flex', alignItems:'center', gap:8 }}>
          <TrineMark color={tk.accent} glow size={22}/>
          <div style={{ color:tk.text, fontWeight:600, fontSize:17, letterSpacing:0.3 }}>Trine Signer</div>
        </div>
        <div style={{ display:'flex', gap:8, alignItems:'center' }}>
          <CoordPill tk={tk}/>
          <button style={{
            width:34, height:34, borderRadius:17, background:tk.surface,
            border:`0.5px solid ${tk.hairline}`, color:tk.text2, cursor:'pointer',
            display:'flex', alignItems:'center', justifyContent:'center',
          }}>{Icon.refresh(15, tk.text2)}</button>
        </div>
      </div>

      {/* Hero count */}
      <div style={{ padding:'14px 18px 0' }}>
        <div style={{ display:'flex', alignItems:'baseline', gap:10 }}>
          <span style={{ color:tk.text, fontSize:42, fontWeight:700, letterSpacing:-1, fontVariantNumeric:'tabular-nums' }}>
            {pending.length}
          </span>
          <span style={{ color:tk.text2, fontSize:15, fontWeight:500 }}>个待签名请求</span>
        </div>
        <div style={{ color:tk.text3, fontSize:12, marginTop:4 }}>
          身份 <span style={{ fontFamily:'ui-monospace, SF Mono, monospace', color:tk.text2 }}>{MEMBER.memberId}</span>
          {' · '}
          relay <span style={{ fontFamily:'ui-monospace, SF Mono, monospace', color:tk.text2 }}>{COORD.relayPeerID}</span>
        </div>
      </div>

      {/* Filter chips */}
      <div style={{ padding:'18px 18px 0', display:'flex', gap:6 }}>
        {['全部 · '+pending.length, '需我决策 · '+pending.filter(e=>e.decisions.find(d=>d.self)?.state==='pending').length, '可疑 · '+pending.filter(e=>!e.crossCheckOk).length].map((c,i)=>(
          <div key={c} style={{
            padding:'7px 13px', borderRadius:99, fontSize:11.5, fontWeight:600, letterSpacing:0.3,
            background: i===0 ? `${tk.accent}1a` : tk.surface,
            color: i===0 ? tk.accent : tk.text2,
            border:`0.5px solid ${i===0 ? tk.accent+'40' : tk.hairline}`,
            whiteSpace:'nowrap',
          }}>{c}</div>
        ))}
      </div>

      {/* Cards */}
      <div style={{ padding:'14px 18px 0', display:'flex', flexDirection:'column', gap:10 }}>
        {pending.map(env => {
          const group = GROUPS.find(g=>g.groupId === env.groupId);
          const myDec = env.decisions.find(d=>d.self);
          const approvedCount = env.decisions.filter(d=>d.state==='approved').length;
          const danger = !env.crossCheckOk;
          return (
            <div key={env.requestId} onClick={()=>nav('detail', env)} style={{
              padding:'14px 14px', borderRadius:18, cursor:'pointer',
              background: danger
                ? `linear-gradient(180deg, ${tk.danger}10, ${tk.surface})`
                : tk.surface,
              border:`0.5px solid ${danger ? tk.danger+'55' : tk.hairline}`,
            }}>
              {/* Top row: chain, status, time */}
              <div style={{ display:'flex', alignItems:'center', gap:8, marginBottom:10 }}>
                <ChainBadge tk={tk} chain={env.chain} label={env.chainLabel}/>
                <span style={{ color:tk.text3, fontSize:11 }}>{group.moniker}</span>
                <div style={{ flex:1 }}/>
                <div style={{ display:'flex', alignItems:'center', gap:4, color:tk.warn, fontSize:11, fontWeight:600 }}>
                  {Icon.clock(12, tk.warn)}
                  {fmtExpiry(env.expiresIn)}
                </div>
              </div>

              {/* Title + amount */}
              <div style={{ display:'flex', alignItems:'flex-start', gap:10, marginBottom:10 }}>
                <div style={{ flex:1, minWidth:0 }}>
                  <div style={{ color:tk.text, fontSize:15, fontWeight:600, letterSpacing:0.1, marginBottom:4,
                    overflow:'hidden', textOverflow:'ellipsis', whiteSpace:'nowrap' }}>
                    {env.businessInfo.title}
                  </div>
                  <div style={{ color:tk.text3, fontSize:11.5, fontFamily:'ui-monospace, SF Mono, monospace' }}>
                    → {env.unsignedTxSummary.to} <span style={{ color: env.unsignedTxSummary.toLabel.startsWith('⚠') ? tk.danger : tk.text3 }}>· {env.unsignedTxSummary.toLabel}</span>
                  </div>
                </div>
                <div style={{ textAlign:'right' }}>
                  <div style={{ color:tk.text, fontSize:15, fontWeight:700, fontVariantNumeric:'tabular-nums', letterSpacing:-0.2 }}>
                    {env.unsignedTxSummary.value}
                  </div>
                  <div style={{ color:tk.text3, fontSize:11, marginTop:1 }}>{env.unsignedTxSummary.valueFiat}</div>
                </div>
              </div>

              {/* Mismatch banner */}
              {danger && (
                <div style={{
                  display:'flex', alignItems:'center', gap:7,
                  padding:'7px 10px', borderRadius:9, marginBottom:10,
                  background:`${tk.danger}1f`, border:`0.5px solid ${tk.danger}55`,
                  color:tk.danger, fontSize:11.5, fontWeight:600,
                }}>
                  {Icon.warn(14, tk.danger)}
                  <span>{env.mismatchHint}</span>
                </div>
              )}

              {/* Foot row: decisions + my status */}
              <div style={{ display:'flex', alignItems:'center', justifyContent:'space-between', paddingTop:10,
                borderTop:`0.5px solid ${tk.hairline}` }}>
                <div style={{ display:'flex', alignItems:'center', gap:8 }}>
                  <div style={{ display:'flex', gap:3 }}>
                    {env.decisions.map(d=>(
                      <div key={d.id} style={{
                        width:18, height:18, borderRadius:5,
                        background: d.state==='approved' ? `${tk.accent}30`
                                 : d.state==='rejected' ? `${tk.danger}30`
                                 : tk.surface2,
                        border:`0.5px solid ${d.state==='approved' ? tk.accent+'66'
                          : d.state==='rejected' ? tk.danger+'66'
                          : tk.hairline}`,
                        display:'flex', alignItems:'center', justifyContent:'center',
                        fontSize:8.5, fontWeight:700, letterSpacing:0.2,
                        color: d.state==='approved' ? tk.accent
                             : d.state==='rejected' ? tk.danger
                             : tk.text3,
                      }}>{d.id}</div>
                    ))}
                  </div>
                  <span style={{ color:tk.text3, fontSize:11 }}>
                    {approvedCount} / {group.threshold} 已批
                  </span>
                </div>
                <div style={{ display:'flex', alignItems:'center', gap:5,
                  color: myDec.state==='pending' ? tk.accent : tk.text3,
                  fontSize:11.5, fontWeight:600 }}>
                  {myDec.state==='pending' ? '需要我决策' : '已决策'}
                  {Icon.chevronR(12, myDec.state==='pending' ? tk.accent : tk.text3)}
                </div>
              </div>
            </div>
          );
        })}

        {pending.length === 0 && (
          <div style={{ padding:'48px 24px', textAlign:'center' }}>
            <div style={{
              width:60, height:60, borderRadius:30, margin:'0 auto 14px',
              background:`${tk.accent}10`, border:`0.5px solid ${tk.accent}33`,
              display:'flex', alignItems:'center', justifyContent:'center',
            }}>{Icon.check(28, tk.accent)}</div>
            <div style={{ color:tk.text, fontSize:15, fontWeight:600 }}>暂无待签请求</div>
            <div style={{ color:tk.text2, fontSize:12, marginTop:6, lineHeight:1.5 }}>
              我们会经由长轮询从 coord 拉取<br/>新请求会推送到此
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

Object.assign(window, { BottomTabs, InboxTab, ChainBadge, CoordPill, fmtExpiry });
