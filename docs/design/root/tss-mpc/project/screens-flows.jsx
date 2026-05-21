// screens-flows.jsx — Keygen wizard, Reshare wizard, Backup, Group detail, Onboarding

// ─────────────────────────────────────────────────────────────
// Wallet detail (formerly Group detail) — inline-editable name,
// addresses list, derive-address sheet
// ─────────────────────────────────────────────────────────────
function GroupDetailScreen({ tk, nav, group, onRename }) {
  if (!group) group = WALLETS[0];
  const [editing, setEditing] = React.useState(false);
  const [name, setName] = React.useState(group.moniker);
  const [showDerive, setShowDerive] = React.useState(false);

  React.useEffect(()=>{ setName(group.moniker); }, [group.moniker]);

  const commitName = () => {
    setEditing(false);
    const trimmed = name.trim();
    if (trimmed && trimmed !== group.moniker) {
      group.moniker = trimmed;     // mutate in place — quick prototype
      if (onRename) onRename(group, trimmed);
    } else {
      setName(group.moniker);
    }
  };

  return (
    <div style={{ height:'100%', display:'flex', flexDirection:'column', position:'relative' }}>
      {/* nav row — back + ellipsis */}
      <div style={{ padding:'58px 16px 6px', display:'flex', alignItems:'center', justifyContent:'space-between' }}>
        <button onClick={()=>nav('groups')} style={{
          width:36, height:36, borderRadius:18, background:tk.surface,
          border:`0.5px solid ${tk.hairline}`, color:tk.text, cursor:'pointer',
          display:'flex', alignItems:'center', justifyContent:'center',
        }}>{Icon.chevronL(18, tk.text)}</button>
        <span style={{ color:tk.text3, fontSize:11, fontWeight:600, letterSpacing:0.3 }}>
          {group.threshold}-of-{group.parties}
        </span>
        <button style={{
          width:36, height:36, borderRadius:18, background:tk.surface,
          border:`0.5px solid ${tk.hairline}`, color:tk.text, cursor:'pointer',
          display:'flex', alignItems:'center', justifyContent:'center',
        }}>{Icon.more(18, tk.text)}</button>
      </div>

      {/* Editable title */}
      <div style={{ padding:'12px 22px 0', display:'flex', alignItems:'center', gap:10 }}>
        {editing ? (
          <input
            autoFocus
            value={name}
            onChange={e=>setName(e.target.value)}
            onBlur={commitName}
            onKeyDown={e=>{ if (e.key==='Enter') commitName(); if (e.key==='Escape'){ setName(group.moniker); setEditing(false); } }}
            maxLength={30}
            style={{
              flex:1, minWidth:0, background:'transparent', border:'none', outline:'none',
              color:tk.text, fontSize:28, fontWeight:700, letterSpacing:-0.5,
              fontFamily:'inherit',
              borderBottom:`1px dashed ${tk.accent}`,
              padding:'0 0 4px',
            }}/>
        ) : (
          <div onClick={()=>setEditing(true)} style={{
            flex:1, minWidth:0, color:tk.text, fontSize:28, fontWeight:700,
            letterSpacing:-0.5, lineHeight:1.2, cursor:'pointer',
            overflow:'hidden', textOverflow:'ellipsis', whiteSpace:'nowrap',
          }}>{group.moniker}</div>
        )}
        <button onClick={()=> editing ? commitName() : setEditing(true)} style={{
          width:32, height:32, borderRadius:10, background:tk.surface,
          border:`0.5px solid ${tk.hairline}`, color: editing ? tk.accent : tk.text2,
          cursor:'pointer', display:'flex', alignItems:'center', justifyContent:'center',
        }}>
          {editing ? Icon.check(15, tk.accent) : (
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none">
              <path d="M14 4l6 6-10 10H4v-6L14 4z" stroke={tk.text2} strokeWidth="1.6" strokeLinejoin="round"/>
            </svg>
          )}
        </button>
      </div>
      <div style={{ padding:'4px 22px 0', color:tk.text3, fontSize:11.5,
        fontFamily:'ui-monospace, SF Mono, monospace' }}>{group.groupId}</div>

      <div style={{ flex:1, overflow:'auto', padding:'18px 18px 28px' }}>
        {/* Addresses */}
        <SectionLabel tk={tk} right={
          <span style={{ color:tk.accent, fontWeight:700 }}>
            {group.addresses.length} 个
          </span>
        }>地址</SectionLabel>
        <div style={{ display:'flex', flexDirection:'column', gap:8, marginBottom:14 }}>
          {group.addresses.map(a => (
            <div key={a.id} style={{
              padding:'12px 14px', borderRadius:14,
              background:tk.surface, border:`0.5px solid ${a.isDefault ? tk.accent+'33' : tk.hairline}`,
            }}>
              <div style={{ display:'flex', alignItems:'center', gap:8, marginBottom:8 }}>
                <ChainBadge tk={tk} chain={a.chain} label={a.chainLabel} small/>
                <span style={{ color:tk.text, fontSize:13, fontWeight:600 }}>{a.label}</span>
                {a.isDefault && (
                  <span style={{
                    padding:'1px 6px', borderRadius:5,
                    background:`${tk.accent}1a`, color:tk.accent,
                    fontSize:9.5, fontWeight:700, letterSpacing:0.4,
                  }}>默认</span>
                )}
                <div style={{ flex:1 }}/>
                <span style={{
                  color:tk.text3, fontSize:10,
                  fontFamily:'ui-monospace, SF Mono, monospace',
                  padding:'2px 6px', borderRadius:5, background:tk.surface2,
                }}>{a.path}</span>
              </div>
              <div style={{ display:'flex', alignItems:'center', gap:8 }}>
                <span style={{
                  flex:1, color:tk.text2, fontSize:12,
                  fontFamily:'ui-monospace, SF Mono, monospace',
                  overflow:'hidden', textOverflow:'ellipsis', whiteSpace:'nowrap',
                }}>{a.address}</span>
                <button style={{
                  width:28, height:28, borderRadius:7, background:tk.surface2,
                  border:`0.5px solid ${tk.hairline}`, color:tk.text2, cursor:'pointer',
                  display:'flex', alignItems:'center', justifyContent:'center',
                }}>{Icon.copy(13, tk.text2)}</button>
                <button style={{
                  width:28, height:28, borderRadius:7, background:tk.surface2,
                  border:`0.5px solid ${tk.hairline}`, color:tk.text2, cursor:'pointer',
                  display:'flex', alignItems:'center', justifyContent:'center',
                }}>{Icon.qr(14, tk.text2)}</button>
              </div>
            </div>
          ))}

          {/* derive new */}
          <div onClick={()=>setShowDerive(true)} style={{
            padding:'12px 14px', borderRadius:14, cursor:'pointer',
            background:tk.surface, border:`0.5px dashed ${tk.hairline}`,
            display:'flex', alignItems:'center', gap:10,
          }}>
            <div style={{
              width:30, height:30, borderRadius:8, background:`${tk.accent}10`,
              border:`0.5px solid ${tk.accent}33`, color:tk.accent,
              display:'flex', alignItems:'center', justifyContent:'center',
            }}>{Icon.plus(15, tk.accent)}</div>
            <div style={{ flex:1 }}>
              <div style={{ color:tk.text, fontSize:13, fontWeight:600 }}>派生新地址 (B8)</div>
              <div style={{ color:tk.text3, fontSize:10.5, marginTop:2 }}>本地 HD 派生 · 不与 coord 交互</div>
            </div>
            {Icon.chevronR(13, tk.text3)}
          </div>
        </div>

        {/* Group public key */}
        <SectionLabel tk={tk}>组公钥</SectionLabel>
        <Card tk={tk} style={{ padding:'2px 14px', marginBottom:14 }}>
          <KV tk={tk} k="ecdsa pubkey" v={group.ecdsaPubkey} mono/>
          <div style={{ borderTop:`0.5px solid ${tk.hairline}` }}/>
          <KV tk={tk} k="chaincode"    v={group.chaincode} mono sub="BIP32 派生用"/>
        </Card>

        {/* Members */}
        <SectionLabel tk={tk}>成员 (memberSet)</SectionLabel>
        <Card tk={tk} style={{ marginBottom:14 }}>
          {group.members.map((m,i)=>(
            <div key={m.id} style={{
              display:'flex', alignItems:'center', gap:11, padding:'12px 14px',
              borderBottom: i<group.members.length-1 ? `0.5px solid ${tk.hairline}` : 'none',
            }}>
              <div style={{
                width:34, height:34, borderRadius:10,
                background: m.self ? `${tk.accent}1a` : tk.surface2,
                border:`0.5px solid ${m.self ? tk.accent+'40' : tk.hairline}`,
                display:'flex', alignItems:'center', justifyContent:'center',
                color: m.self ? tk.accent : tk.text2,
                fontSize:12, fontWeight:700, fontFamily:'ui-monospace, SF Mono, monospace',
              }}>{m.id}</div>
              <div style={{ flex:1 }}>
                <div style={{ color:tk.text, fontSize:13.5, fontWeight:500 }}>
                  {m.label} {m.self && <span style={{ color:tk.accent }}>· 本机</span>}
                </div>
                <div style={{ color:tk.text3, fontSize:10.5, marginTop:2 }}>
                  partyIndex {i} · 上次见 {m.last}
                </div>
              </div>
              <div style={{ display:'flex', alignItems:'center', gap:5 }}>
                <div style={{ width:6, height:6, borderRadius:3,
                  background: m.status==='online' ? tk.accent : m.status==='offline' ? tk.danger : tk.text3 }}/>
                <span style={{
                  color: m.status==='online' ? tk.accent : m.status==='offline' ? tk.danger : tk.text3,
                  fontSize:10.5, fontWeight:600,
                }}>{m.status==='online'?'在线':m.status==='offline'?'离线':'待机'}</span>
              </div>
            </div>
          ))}
        </Card>

        <SectionLabel tk={tk}>操作</SectionLabel>
        <Card tk={tk}>
          {[
            { l:'触发 reshare',     sub:'轮换分片 · 组公钥保持不变', go:'reshare' },
            { l:'attestation 历史', sub:'记录最近一次三方在场证明', go:null },
            { l:'移除此钱包',       sub:'仅本机离线 · 不影响其他成员', danger:true, go:null },
          ].map((r,i,a)=>(
            <div key={i} onClick={()=>r.go && nav(r.go, group)} style={{
              padding:'12px 14px', display:'flex', alignItems:'center', gap:10,
              borderBottom: i<a.length-1 ? `0.5px solid ${tk.hairline}` : 'none',
              cursor: r.go ? 'pointer' : 'default',
            }}>
              <div style={{ flex:1 }}>
                <div style={{ color: r.danger ? tk.danger : tk.text,
                  fontSize:13.5, fontWeight:500 }}>{r.l}</div>
                <div style={{ color:tk.text3, fontSize:10.5, marginTop:2 }}>{r.sub}</div>
              </div>
              {Icon.chevronR(12, tk.text3)}
            </div>
          ))}
        </Card>
      </div>

      {/* Derive bottom sheet */}
      {showDerive && <DeriveAddressSheet tk={tk} group={group} onClose={()=>setShowDerive(false)}/>}
    </div>
  );
}

// ─────────────────────────────────────────────────────────────
// Derive address bottom sheet
// ─────────────────────────────────────────────────────────────
function DeriveAddressSheet({ tk, group, onClose }) {
  const [chain, setChain] = React.useState('eip155:1');
  const [pathIdx, setPathIdx] = React.useState(group.addresses.length);
  const [label, setLabel] = React.useState('');

  const chains = [
    { id:'eip155:1',     label:'Ethereum' },
    { id:'eip155:42161', label:'Arbitrum' },
    { id:'tron',         label:'TRON' },
  ];
  const path = `m/0/${pathIdx}`;

  // Preview a deterministic-looking address based on chain + index
  const preview = chain === 'tron'
    ? 'T' + 'XzKfP9wMNqR4VeBu'.slice(0, 10) + (pathIdx*7 + 13).toString(16).padStart(4,'0') + '...8jKp'
    : '0x' + (pathIdx*0xb71d + 0x4a3c).toString(16).padStart(6,'0') + 'fA3CdB91...e7c' + (pathIdx*3).toString(16).padStart(2,'0');

  return (
    <div style={{
      position:'absolute', inset:0, zIndex:100,
      background:'rgba(0,0,0,0.55)',
      display:'flex', flexDirection:'column',
    }}>
      <div onClick={onClose} style={{ flex:1, cursor:'pointer' }}/>
      <div style={{
        background:tk.surface, borderTopLeftRadius:28, borderTopRightRadius:28,
        padding:'14px 22px 36px', boxShadow:'0 -8px 40px rgba(0,0,0,0.55)',
        animation:'trineSheetUp 0.28s ease-out',
      }}>
        <div style={{ width:38, height:4.5, borderRadius:3, background:tk.surface2, margin:'0 auto 16px' }}/>

        <div style={{ display:'flex', alignItems:'center', justifyContent:'space-between', marginBottom:6 }}>
          <div style={{ color:tk.text, fontSize:19, fontWeight:700, letterSpacing:-0.3 }}>派生新地址</div>
          <button onClick={onClose} style={{
            width:30, height:30, borderRadius:15, background:tk.surface2,
            border:'none', color:tk.text2, cursor:'pointer',
            display:'flex', alignItems:'center', justifyContent:'center',
          }}>{Icon.close(15, tk.text2)}</button>
        </div>
        <div style={{ color:tk.text3, fontSize:11.5, lineHeight:1.5, marginBottom:18 }}>
          从本组 xpub 离线 HD 派生 · 无需 coord 交互
        </div>

        {/* chain picker */}
        <SectionLabel tk={tk}>链</SectionLabel>
        <div style={{ display:'flex', gap:6, marginBottom:16 }}>
          {chains.map(c=>(
            <button key={c.id} onClick={()=>setChain(c.id)} style={{
              flex:1, padding:'10px 0', borderRadius:11,
              background: chain===c.id ? `${tk.accent}18` : tk.surface2,
              border:`0.5px solid ${chain===c.id ? tk.accent+'55' : tk.hairline}`,
              color: chain===c.id ? tk.accent : tk.text2,
              fontSize:12, fontWeight:600, letterSpacing:0.3, cursor:'pointer',
              fontFamily:'inherit',
            }}>{c.label}</button>
          ))}
        </div>

        {/* path / index */}
        <SectionLabel tk={tk}>派生路径</SectionLabel>
        <div style={{
          padding:'12px 14px', borderRadius:13, marginBottom:16,
          background:tk.surface2, border:`0.5px solid ${tk.hairline}`,
          display:'flex', alignItems:'center', gap:12,
        }}>
          <span style={{
            color:tk.text2, fontSize:14,
            fontFamily:'ui-monospace, SF Mono, monospace', letterSpacing:0.4,
          }}>m/0/</span>
          <div style={{ flex:1, display:'flex', alignItems:'center', gap:10 }}>
            <button onClick={()=>setPathIdx(Math.max(0, pathIdx-1))} style={{
              width:28, height:28, borderRadius:7, background:tk.surface,
              border:`0.5px solid ${tk.hairline}`, color:tk.text, cursor:'pointer',
              fontSize:16, fontWeight:600, fontFamily:'inherit',
            }}>−</button>
            <span style={{
              flex:1, textAlign:'center',
              color:tk.accent, fontSize:18, fontWeight:700,
              fontFamily:'ui-monospace, SF Mono, monospace',
              fontVariantNumeric:'tabular-nums',
            }}>{pathIdx}</span>
            <button onClick={()=>setPathIdx(pathIdx+1)} style={{
              width:28, height:28, borderRadius:7, background:tk.surface,
              border:`0.5px solid ${tk.hairline}`, color:tk.text, cursor:'pointer',
              fontSize:16, fontWeight:600, fontFamily:'inherit',
            }}>+</button>
          </div>
        </div>

        {/* label */}
        <SectionLabel tk={tk}>名称 (可选)</SectionLabel>
        <div style={{
          padding:'12px 14px', borderRadius:13, marginBottom:18,
          background:tk.surface2, border:`0.5px solid ${tk.hairline}`,
        }}>
          <input
            value={label}
            onChange={e=>setLabel(e.target.value)}
            placeholder="例如:运营提现 · 周结"
            maxLength={20}
            style={{
              width:'100%', background:'transparent', border:'none', outline:'none',
              color:tk.text, fontSize:14, fontFamily:'inherit',
            }}/>
        </div>

        {/* preview */}
        <div style={{
          padding:'14px 14px', borderRadius:13, marginBottom:18,
          background:`${tk.accent}08`, border:`0.5px solid ${tk.accent}33`,
        }}>
          <div style={{ display:'flex', alignItems:'center', gap:8, marginBottom:8 }}>
            <ChainBadge tk={tk} chain={chain} label={chains.find(c=>c.id===chain).label} small/>
            <span style={{ color:tk.text2, fontSize:11,
              fontFamily:'ui-monospace, SF Mono, monospace' }}>{path}</span>
            <div style={{ flex:1 }}/>
            <span style={{ color:tk.text3, fontSize:10, letterSpacing:0.5 }}>预览</span>
          </div>
          <div style={{
            color:tk.text, fontSize:13, fontWeight:600,
            fontFamily:'ui-monospace, SF Mono, monospace',
            wordBreak:'break-all', lineHeight:1.45,
          }}>{preview}</div>
        </div>

        <PrimaryBtn tk={tk} onClick={()=>{
          // mutate in place — quick prototype
          group.addresses.push({
            id:'a'+Date.now(),
            label: label || `派生地址 #${pathIdx}`,
            chain, chainLabel: chains.find(c=>c.id===chain).label,
            path, address: preview,
          });
          onClose();
        }}>添加到钱包</PrimaryBtn>
      </div>
    </div>
  );
}

// ─────────────────────────────────────────────────────────────
// Keygen / Reshare wizard
// ─────────────────────────────────────────────────────────────
function KeygenWizard({ tk, nav, mode='keygen' }) {
  const isReshare = mode === 'reshare';
  const [phase] = useSigningPhase(true, 6200);
  const stage = phase < 0.18 ? 0 : phase < 0.36 ? 1 : phase < 0.6 ? 2 : phase < 0.85 ? 3 : 4;

  const stages = isReshare ? [
    { label:'拉取旧组配置',      sub:'GET /v1/groups/{gid}/config' },
    { label:'attestation 三方在线', sub:'B11 · 旧持有方互证存活' },
    { label:'轮换 polynomial',    sub:'Round R1 · 各方重采样 fᵢ' },
    { label:'分发新分片',         sub:'Round R2 · 加密交换 fᵢ(j)' },
    { label:'验证公钥不变',       sub:'Y_new == Y_old · 提交 commit' },
  ] : [
    { label:'接收 START',         sub:'B6 dispatch · configJSON 已校验' },
    { label:'参与方互通',         sub:'Round 0 · gossip identity · n=3' },
    { label:'承诺值 (round 1)',   sub:'广播 (Cᵢ, Aᵢ) · feldman commit' },
    { label:'分片分发 (round 2)', sub:'对每个 j ≠ i 发送 fᵢ(j) (加密)' },
    { label:'写入 keystore',      sub:'shareᵢ · Argon2id 封装 · 落盘' },
  ];

  React.useEffect(() => {
    if (phase >= 1) {
      const t = setTimeout(() => nav('keygenDone', { mode }), 600);
      return () => clearTimeout(t);
    }
  }, [phase]);

  return (
    <div style={{ height:'100%', display:'flex', flexDirection:'column' }}>
      <TopBar tk={tk} title={isReshare ? 'Reshare 进行中' : 'Keygen · DKG'} onBack={()=>nav(isReshare?'groups':'groups')}/>

      <div style={{ flex:1, overflow:'auto', padding:'4px 22px 24px' }}>
        {/* config card */}
        <Card tk={tk} style={{ padding:'14px 14px', marginBottom:18 }}>
          <div style={{ color:tk.text3, fontSize:10.5, fontWeight:600, letterSpacing:0.6, textTransform:'uppercase', marginBottom:6 }}>configJSON (DM-3)</div>
          <div style={{
            color:tk.text2, fontSize:11,
            fontFamily:'ui-monospace, SF Mono, monospace',
            lineHeight:1.55,
          }}>
            {'{\n'}
            <span style={{ color:tk.text3 }}>{'  '}groupId:</span>{' '}<span style={{ color:tk.accent }}>"grp_5a8e7b3c"</span>,{'\n'}
            <span style={{ color:tk.text3 }}>{'  '}sessionID:</span>{' '}<span style={{ color:tk.accent }}>"kg_2026-05-21"</span>,{'\n'}
            <span style={{ color:tk.text3 }}>{'  '}partyIndex:</span>{' '}<span style={{ color:tk.accent }}>0</span>,{'\n'}
            <span style={{ color:tk.text3 }}>{'  '}n:</span>{' '}<span style={{ color:tk.accent }}>3</span>,{' '}
            <span style={{ color:tk.text3 }}>t:</span>{' '}<span style={{ color:tk.accent }}>1</span>,{'\n'}
            <span style={{ color:tk.text3 }}>{'  '}memberSet:</span>{' '}<span style={{ color:tk.accent }}>["m0", "m1", "m2"]</span>,{'\n'}
            <span style={{ color:tk.text3 }}>{'  '}role:</span>{' '}<span style={{ color:tk.accent }}>"member"</span>{'\n'}
            {'}'}
          </div>
        </Card>

        {/* progress steps */}
        <div style={{ display:'flex', flexDirection:'column', gap:10 }}>
          {stages.map((st, i) => {
            const done = i < stage;
            const active = i === stage;
            return (
              <div key={i} style={{
                display:'flex', alignItems:'center', gap:14,
                padding:'12px 14px', borderRadius:14,
                background: active ? `${tk.accent}10` : tk.surface,
                border:`0.5px solid ${active ? tk.accent+'55' : tk.hairline}`,
                opacity: done || active ? 1 : 0.55,
              }}>
                {/* indicator */}
                <div style={{
                  width:30, height:30, borderRadius:15,
                  background: done ? tk.accent : active ? `${tk.accent}22` : tk.surface2,
                  border: active ? `1.5px solid ${tk.accent}` : `0.5px solid ${tk.hairline}`,
                  display:'flex', alignItems:'center', justifyContent:'center', flexShrink:0,
                  color: done ? '#0A0F1C' : tk.accent,
                  fontSize:12, fontWeight:700,
                  position:'relative',
                }}>
                  {done ? Icon.check(15, '#0A0F1C') : (
                    <>
                      {active && (
                        <div style={{
                          position:'absolute', inset:-3, borderRadius:20,
                          border:`1.5px solid ${tk.accent}`, opacity:0.4,
                          animation:'trinePulse 1.6s ease-in-out infinite',
                        }}/>
                      )}
                      {i+1}
                    </>
                  )}
                </div>
                <div style={{ flex:1 }}>
                  <div style={{ color: active ? tk.text : (done ? tk.text2 : tk.text3),
                    fontSize:13.5, fontWeight:600 }}>{st.label}</div>
                  <div style={{ color:tk.text3, fontSize:10.5, marginTop:2,
                    fontFamily:'ui-monospace, SF Mono, monospace' }}>{st.sub}</div>
                </div>
                {active && (
                  <div style={{ width:6, height:6, borderRadius:3, background:tk.accent,
                    animation:'trinePulse 1s ease-in-out infinite' }}/>
                )}
              </div>
            );
          })}
        </div>

        <div style={{
          marginTop:18, padding:'10px 14px',
          background: tk.surface, borderRadius:12,
          border:`0.5px solid ${tk.hairline}`,
          display:'flex', alignItems:'flex-start', gap:8,
        }}>
          {Icon.info(15, tk.text2)}
          <div style={{ flex:1, color:tk.text2, fontSize:11, lineHeight:1.5 }}>
            {isReshare
              ? '分片轮换期间设备需保持联网。新分片会替代旧分片;组公钥与地址保持不变。'
              : '请保持本设备与 m1 / m2 同时在线 — 任一掉线本轮 DKG 会回滚,需重新发起。'}
          </div>
        </div>
      </div>

      <div style={{ padding:'12px 22px 36px' }}>
        <GhostBtn tk={tk} onClick={()=>nav('groups')} style={{ width:'100%' }}>取消会话</GhostBtn>
      </div>
    </div>
  );
}

// ─────────────────────────────────────────────────────────────
// Keygen done screen
// ─────────────────────────────────────────────────────────────
function KeygenDoneScreen({ tk, nav, mode='keygen' }) {
  const isReshare = mode === 'reshare';
  return (
    <div style={{ height:'100%', display:'flex', flexDirection:'column' }}>
      <div style={{ flex:1, padding:'0 24px', display:'flex', flexDirection:'column',
        justifyContent:'center', alignItems:'center' }}>
        <TrineMark color={tk.accent} size={92} glow/>

        <div style={{ color:tk.text, fontSize:24, fontWeight:700, marginTop:26,
          letterSpacing:-0.4, textAlign:'center' }}>
          {isReshare ? '分片已轮换' : '钱包已创建'}
        </div>
        <div style={{ color:tk.text2, fontSize:13, marginTop:8, textAlign:'center', lineHeight:1.5 }}>
          {isReshare
            ? '组公钥与所有地址保持不变\n旧分片已 zeroize'
            : '已生成 ECDSA 组公钥\nshare₀ 已用 Argon2id 封装并落 keystore'}
        </div>

        <Card tk={tk} style={{ width:'100%', marginTop:24, padding:'2px 14px' }}>
          <KV tk={tk} k="groupId"     v="grp_5a8e7b3c" mono/>
          <div style={{ borderTop:`0.5px solid ${tk.hairline}` }}/>
          <KV tk={tk} k="阈值"        v="2-of-3"/>
          <div style={{ borderTop:`0.5px solid ${tk.hairline}` }}/>
          <KV tk={tk} k="ecdsa pubkey" v="02f8a3...c4e1" mono/>
          <div style={{ borderTop:`0.5px solid ${tk.hairline}` }}/>
          <KV tk={tk} k="EVM 地址"    v="0xA4f3CBd2...23E89bF1" mono/>
        </Card>
      </div>

      <div style={{ padding:'14px 22px 38px', display:'flex', flexDirection:'column', gap:8 }}>
        <PrimaryBtn tk={tk} onClick={()=>nav('backup')}>立即备份分片</PrimaryBtn>
        <button onClick={()=>nav('groups')} style={{
          width:'100%', height:48, background:'none', border:'none',
          color:tk.text2, fontSize:14, fontWeight:500, cursor:'pointer', fontFamily:'inherit',
        }}>稍后</button>
      </div>
    </div>
  );
}

// ─────────────────────────────────────────────────────────────
// Backup screen
// ─────────────────────────────────────────────────────────────
function BackupScreen({ tk, nav }) {
  const [pass, setPass] = React.useState('');
  return (
    <div style={{ height:'100%', display:'flex', flexDirection:'column' }}>
      <TopBar tk={tk} title="导出分片备份" onBack={()=>nav('settings')}/>

      <div style={{ flex:1, overflow:'auto', padding:'4px 22px 24px' }}>
        <div style={{ padding:'14px 14px', borderRadius:14, marginBottom:18,
          background:`${tk.warn}10`, border:`0.5px solid ${tk.warn}40` }}>
          <div style={{ display:'flex', alignItems:'center', gap:7, marginBottom:6 }}>
            {Icon.warn(15, tk.warn)}
            <span style={{ color:tk.warn, fontSize:12.5, fontWeight:700 }}>请离线保管这份备份</span>
          </div>
          <div style={{ color:tk.text2, fontSize:11.5, lineHeight:1.55 }}>
            分片仅是 t-of-n 的一片 — 单独无法签名,但与口令一起足以让攻击者参与 keygen 之后的所有签名会话。请存放在 air-gapped 介质。
          </div>
        </div>

        <SectionLabel tk={tk}>导出参数</SectionLabel>
        <Card tk={tk} style={{ padding:'2px 14px', marginBottom:18 }}>
          <KV tk={tk} k="钱包"     v="财库主钱包" sub="grp_5a8e7b3c"/>
          <div style={{ borderTop:`0.5px solid ${tk.hairline}` }}/>
          <KV tk={tk} k="moniker"  v="m0"      mono/>
          <div style={{ borderTop:`0.5px solid ${tk.hairline}` }}/>
          <KV tk={tk} k="封装算法" v="Argon2id (32 MiB · t=3)" accent sub="对称封装 · 绝不明文持有"/>
        </Card>

        <SectionLabel tk={tk}>设置封装口令</SectionLabel>
        <div style={{
          padding:'14px 14px', borderRadius:14, marginBottom:10,
          background:tk.surface, border:`0.5px solid ${tk.hairline}`,
        }}>
          <input
            type="password"
            value={pass}
            onChange={e=>setPass(e.target.value)}
            placeholder="至少 12 位 · 与 keystore 口令分离"
            style={{
              width:'100%', background:'transparent', border:'none', outline:'none',
              color:tk.text, fontSize:15, fontWeight:500,
              fontFamily:'ui-monospace, SF Mono, monospace', letterSpacing:0.5,
            }}/>
          {/* strength bar */}
          <div style={{ display:'flex', gap:4, marginTop:10 }}>
            {[0,1,2,3].map(i=>(
              <div key={i} style={{ flex:1, height:3, borderRadius:2,
                background: pass.length >= (i+1)*4 ? tk.accent : tk.surface2 }}/>
            ))}
          </div>
        </div>
        <div style={{ color:tk.text3, fontSize:10.5, padding:'0 4px', marginBottom:14 }}>
          口令永不离开本设备 · 仅用于派生 KEK 封装 share 字节
        </div>
      </div>

      <div style={{ padding:'14px 22px 36px' }}>
        <PrimaryBtn tk={tk} disabled={pass.length < 12} onClick={()=>nav('settings')}>
          导出 backup-m0.bin
        </PrimaryBtn>
      </div>
    </div>
  );
}

// ─────────────────────────────────────────────────────────────
// Onboarding (passphrase + bind coord)
// ─────────────────────────────────────────────────────────────
function OnboardingScreen({ tk, nav }) {
  return (
    <div style={{ height:'100%', display:'flex', flexDirection:'column' }}>
      <div style={{ flex:1, padding:'70px 28px 0', display:'flex', flexDirection:'column' }}>
        <div style={{ flex:1, display:'flex', flexDirection:'column', alignItems:'center', justifyContent:'center', position:'relative' }}>
          <div style={{
            position:'absolute', width:260, height:260, borderRadius:130,
            background:`radial-gradient(circle, ${tk.accent}22, transparent 60%)`, filter:'blur(20px)',
          }}/>
          <div style={{ position:'relative' }}>
            <TrineMark color={tk.accent} glow size={108}/>
          </div>
        </div>
        <div style={{ textAlign:'center', marginBottom:28 }}>
          <div style={{ color:tk.accent, fontSize:11.5, fontWeight:700, letterSpacing:2.5,
            textTransform:'uppercase', marginBottom:14 }}>
            TRINE · MPC SIGNER
          </div>
          <div style={{ color:tk.text, fontSize:32, fontWeight:700, lineHeight:1.15, letterSpacing:-0.7 }}>
            一台设备<br/>
            一片分片<br/>
            一次握手
          </div>
          <div style={{ color:tk.text2, fontSize:13.5, lineHeight:1.55, marginTop:14, padding:'0 8px' }}>
            本机仅作为成员参与 TSS。<br/>
            没有余额 · 没有交易构造 · 不持完整私钥。
          </div>
        </div>
      </div>

      <div style={{ padding:'0 22px 44px', display:'flex', flexDirection:'column', gap:10 }}>
        <PrimaryBtn tk={tk} onClick={()=>nav('initIdentity')}>开始初始化</PrimaryBtn>
        <button onClick={()=>nav('inbox')} style={{
          width:'100%', height:48, background:'none', border:'none',
          color:tk.text2, fontSize:14, fontWeight:500, cursor:'pointer', fontFamily:'inherit',
        }}>从备份恢复分片</button>
      </div>
    </div>
  );
}

Object.assign(window, {
  GroupDetailScreen, KeygenWizard, KeygenDoneScreen, BackupScreen, OnboardingScreen,
});
