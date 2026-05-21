// screens-tabs2.jsx — Groups, Audit, Settings tabs

// ─────────────────────────────────────────────────────────────
// Groups tab
// ─────────────────────────────────────────────────────────────
function GroupsTab({ tk, nav, wallets }) {
  const list = wallets || WALLETS;
  return (
    <div style={{ paddingBottom:120 }}>
      <TopBar tk={tk} title="钱包" right={
        <button style={{ width:36, height:36, borderRadius:18, background:tk.surface, border:`0.5px solid ${tk.hairline}`, color:tk.text, cursor:'pointer', display:'flex', alignItems:'center', justifyContent:'center' }}>{Icon.plus(18, tk.text)}</button>
      }/>

      <div style={{ padding:'8px 18px 0', display:'flex', flexDirection:'column', gap:12 }}>
        {list.map(g => (
          <div key={g.groupId} onClick={()=>nav('group', g)} style={{
            padding:'16px 16px', borderRadius:20, cursor:'pointer',
            background:tk.surface, border:`0.5px solid ${tk.hairline}`,
          }}>
            {/* head */}
            <div style={{ display:'flex', alignItems:'center', gap:10, marginBottom:14 }}>
              <div style={{
                width:36, height:36, borderRadius:11,
                background:`${tk.accent}14`, border:`0.5px solid ${tk.accent}33`,
                display:'flex', alignItems:'center', justifyContent:'center',
              }}><TrineMark color={tk.accent} size={20}/></div>
              <div style={{ flex:1, minWidth:0 }}>
                <div style={{ color:tk.text, fontSize:15, fontWeight:600 }}>{g.moniker}</div>
                <div style={{ color:tk.text3, fontSize:11, marginTop:2,
                  fontFamily:'ui-monospace, SF Mono, monospace' }}>{g.groupId}</div>
              </div>
              <div style={{
                padding:'4px 9px', borderRadius:7,
                background:`${tk.accent}1a`, color:tk.accent,
                fontSize:11, fontWeight:700, letterSpacing:0.4,
              }}>{g.threshold}-of-{g.parties}</div>
            </div>

            {/* members strip */}
            <div style={{ display:'flex', gap:5, marginBottom:12 }}>
              {g.members.map((m,i)=>(
                <div key={m.id} style={{ flex:1, padding:'7px 4px', borderRadius:8,
                  background: m.self ? `${tk.accent}10` : tk.surface2,
                  border:`0.5px solid ${m.self ? tk.accent+'33' : tk.hairline}`,
                  display:'flex', flexDirection:'column', alignItems:'center', gap:3,
                }}>
                  <div style={{ position:'relative' }}>
                    <span style={{ color: m.self ? tk.accent : tk.text2,
                      fontSize:11, fontWeight:700, fontFamily:'ui-monospace, SF Mono, monospace' }}>{m.id}</span>
                    <div style={{
                      position:'absolute', top:-2, right:-7, width:6, height:6, borderRadius:3,
                      background: m.status==='online' ? tk.accent : m.status==='offline' ? tk.danger : tk.text3,
                    }}/>
                  </div>
                  <span style={{ color:tk.text3, fontSize:9.5 }}>{m.self ? '本机' : m.status==='online'?'在线':m.status==='offline'?'离线':'待机'}</span>
                </div>
              ))}
            </div>

            {/* address summary */}
            <div style={{
              display:'flex', alignItems:'center', gap:10,
              padding:'10px 12px', borderRadius:11, background:tk.surface2,
            }}>
              <div style={{ display:'flex', flexDirection:'column', gap:2 }}>
                <span style={{ color:tk.text, fontSize:18, fontWeight:700, fontVariantNumeric:'tabular-nums', lineHeight:1 }}>
                  {g.addresses.length}
                </span>
                <span style={{ color:tk.text3, fontSize:10, letterSpacing:0.3 }}>个地址</span>
              </div>
              <div style={{ width:0.5, height:24, background:tk.hairline }}/>
              <div style={{ flex:1, display:'flex', flexWrap:'wrap', gap:5 }}>
                {[...new Set(g.addresses.map(a=>a.chainLabel))].map(c => (
                  <span key={c} style={{
                    padding:'2px 7px', borderRadius:5, fontSize:10, fontWeight:600,
                    background:tk.surface, color:tk.text2, letterSpacing:0.2,
                    border:`0.5px solid ${tk.hairline}`,
                  }}>{c}</span>
                ))}
              </div>
              {Icon.chevronR(13, tk.text3)}
            </div>
          </div>
        ))}

        {/* keygen call-to-action */}
        <div onClick={()=>nav('keygen')} style={{
          padding:'14px 16px', borderRadius:18, cursor:'pointer',
          background:tk.surface, border:`0.5px dashed ${tk.hairline}`,
          display:'flex', alignItems:'center', gap:12,
        }}>
          <div style={{
            width:36, height:36, borderRadius:11, background:tk.surface2,
            display:'flex', alignItems:'center', justifyContent:'center', color:tk.accent,
          }}>{Icon.plus(18, tk.accent)}</div>
          <div style={{ flex:1 }}>
            <div style={{ color:tk.text, fontSize:14, fontWeight:600 }}>新建钱包 · 启动 keygen</div>
            <div style={{ color:tk.text3, fontSize:11, marginTop:2 }}>从 coord START 接收 DKG 配置</div>
          </div>
          {Icon.chevronR(13, tk.text3)}
        </div>
      </div>
    </div>
  );
}

// ─────────────────────────────────────────────────────────────
// Audit tab
// ─────────────────────────────────────────────────────────────
function AuditTab({ tk, nav }) {
  // group by date
  const byDate = {};
  AUDIT.forEach(r => { (byDate[r.d] = byDate[r.d] || []).push(r); });

  return (
    <div style={{ paddingBottom:120 }}>
      <TopBar tk={tk} title="审计日志" right={
        <button style={{ width:36, height:36, borderRadius:18, background:tk.surface, border:`0.5px solid ${tk.hairline}`, color:tk.text, cursor:'pointer', display:'flex', alignItems:'center', justifyContent:'center' }}>{Icon.more(18, tk.text)}</button>
      }/>

      <div style={{ padding:'4px 18px 0' }}>
        {/* stats */}
        <div style={{ display:'grid', gridTemplateColumns:'repeat(3,1fr)', gap:8, marginBottom:18 }}>
          {[
            { l:'已签名', v:AUDIT.filter(r=>r.op==='signed').length, c:tk.accent },
            { l:'已拒绝', v:AUDIT.filter(r=>r.op==='rejected').length, c:tk.danger },
            { l:'已过期', v:AUDIT.filter(r=>r.op==='expired').length, c:tk.warn },
          ].map(s=>(
            <div key={s.l} style={{ padding:'11px 12px', borderRadius:14,
              background:tk.surface, border:`0.5px solid ${tk.hairline}` }}>
              <div style={{ color:s.c, fontSize:22, fontWeight:700, fontVariantNumeric:'tabular-nums' }}>{s.v}</div>
              <div style={{ color:tk.text3, fontSize:10.5, marginTop:2 }}>{s.l}</div>
            </div>
          ))}
        </div>

        {Object.entries(byDate).map(([date, rows]) => (
          <div key={date} style={{ marginBottom:18 }}>
            <div style={{ color:tk.text2, fontSize:11.5, fontWeight:600, letterSpacing:0.6,
              textTransform:'uppercase', marginBottom:8, paddingLeft:4 }}>{date}</div>
            <Card tk={tk}>
              {rows.map((r,i)=>{
                const cfg = r.op === 'signed'
                  ? { c:tk.accent, label:'已签名', ico:Icon.check }
                  : r.op === 'rejected'
                  ? { c:tk.danger, label:'已拒绝', ico:Icon.close }
                  : { c:tk.warn, label:'已过期', ico:Icon.clock };
                return (
                  <div key={r.requestId} style={{
                    padding:'12px 14px', display:'flex', alignItems:'flex-start', gap:11,
                    borderBottom: i<rows.length-1 ? `0.5px solid ${tk.hairline}` : 'none',
                  }}>
                    <div style={{
                      width:30, height:30, borderRadius:9, flexShrink:0,
                      background:`${cfg.c}14`, color:cfg.c,
                      display:'flex', alignItems:'center', justifyContent:'center',
                      border:`0.5px solid ${cfg.c}33`,
                    }}>{cfg.ico(15, cfg.c)}</div>
                    <div style={{ flex:1, minWidth:0 }}>
                      <div style={{ display:'flex', alignItems:'center', gap:6, marginBottom:2 }}>
                        <span style={{ color:tk.text, fontSize:13, fontWeight:600 }}>{cfg.label}</span>
                        <span style={{ color:tk.text3, fontSize:10.5,
                          fontFamily:'ui-monospace, SF Mono, monospace' }}>{r.requestId}</span>
                      </div>
                      <div style={{ color:tk.text2, fontSize:12, marginBottom:3 }}>
                        {r.value} → <span style={{ fontFamily:'ui-monospace, SF Mono, monospace' }}>{r.to}</span>
                      </div>
                      <div style={{ display:'flex', alignItems:'center', gap:6, color:tk.text3, fontSize:10.5 }}>
                        <span>{r.t}</span>
                        <span>·</span>
                        <span>{r.group}</span>
                        <span>·</span>
                        <span>{r.chain}</span>
                      </div>
                      {r.rsv && (
                        <div style={{ marginTop:6, padding:'4px 8px', borderRadius:6,
                          background:tk.surface2, color:tk.text2, fontSize:10.5,
                          fontFamily:'ui-monospace, SF Mono, monospace',
                          display:'flex', alignItems:'center', gap:5,
                        }}>
                          {Icon.shield(10, tk.accent)}
                          <span>RSV: {r.rsv}</span>
                        </div>
                      )}
                      {r.reason && (
                        <div style={{ marginTop:6, color:cfg.c, fontSize:10.5 }}>{r.reason}</div>
                      )}
                    </div>
                  </div>
                );
              })}
            </Card>
          </div>
        ))}
      </div>
    </div>
  );
}

// ─────────────────────────────────────────────────────────────
// Settings tab
// ─────────────────────────────────────────────────────────────
function SettingsTab({ tk, nav }) {
  const sections = [
    { hd:'身份', rows:[
      { l:'memberId', r:MEMBER.memberId, mono:true },
      { l:'设备', r:MEMBER.device },
      { l:'身份公钥', r:MEMBER.identityPub.slice(0,16)+'…', mono:true, sub:'仅本设备持有私钥' },
    ]},    { hd:'连接', rows:[
      { l:'coord 端点',  r:COORD.endpoint.replace('https://',''), mono:true, sub:`${COORD.tls} · 上次心跳 ${COORD.lastHeartbeat}` },
      { l:'relay peer', r:COORD.relayPeerID, mono:true },
      { l:'B6 调度',    r:'长轮询', accent:true, sub:'同时启用 webhook 旁路' },
    ]},
    { hd:'分片', rows:[
      { l:'导出分片备份', r:'',  onClick:()=>nav('backup'),  sub:'口令封装(Argon2id),绝不明文' },
      { l:'导入分片备份', r:'',  onClick:()=>nav('import') },
      { l:'更改 keystore 口令', r:'' },
      { l:'重分片 (reshare)', r:'', onClick:()=>nav('reshare'), sub:'轮换分片,保持组公钥不变' },
    ]},
    { hd:'安全', rows:[
      { l:'Face ID 解锁审批', r:'已开启', accent:true },
      { l:'审批超时 zeroize', r:'5 分钟' },
      { l:'WYSIWYS 严格模式', r:'已开启', accent:true, sub:'A facts / B info 不匹配时拒绝签名' },
    ]},
    { hd:'其他', rows:[
      { l:'查看 attestation 记录' },
      { l:'诊断 · 日志导出' },
      { l:'关于', r:'v1.0.0 (P4)' },
    ]},
  ];

  return (
    <div style={{ paddingBottom:120 }}>
      <TopBar tk={tk} title="设置"/>

      <div style={{ padding:'4px 18px 0' }}>
        {/* identity card */}
        <Card tk={tk} style={{ padding:'16px 16px', display:'flex', alignItems:'center', gap:14, marginBottom:18 }}>
          <div style={{
            width:50, height:50, borderRadius:14,
            background:`linear-gradient(135deg, ${tk.accent}, #A78BFA)`,
            display:'flex', alignItems:'center', justifyContent:'center',
            color:'#0A0F1C', fontSize:18, fontWeight:700, letterSpacing:-0.3,
            fontFamily:'ui-monospace, SF Mono, monospace',
          }}>{MEMBER.memberId}</div>
          <div style={{ flex:1 }}>
            <div style={{ color:tk.text, fontSize:15, fontWeight:600 }}>{MEMBER.device}</div>
            <div style={{ color:tk.text3, fontSize:11.5, marginTop:3 }}>
              加入 <span style={{ color:tk.text2 }}>{WALLETS.length}</span> 个钱包 · 总参与方 <span style={{ color:tk.text2 }}>{WALLETS.reduce((s,g)=>s+g.parties,0)}</span>
            </div>
          </div>
          <CoordPill tk={tk}/>
        </Card>

        {sections.map(s => (
          <div key={s.hd} style={{ marginBottom:16 }}>
            <div style={{ color:tk.text2, fontSize:11.5, fontWeight:600, letterSpacing:0.6,
              textTransform:'uppercase', marginBottom:8, paddingLeft:4 }}>{s.hd}</div>
            <Card tk={tk}>
              {s.rows.map((r,i)=>(
                <div key={i} onClick={r.onClick} style={{
                  padding:'12px 14px', display:'flex', alignItems:'center', gap:10,
                  borderBottom: i<s.rows.length-1 ? `0.5px solid ${tk.hairline}` : 'none',
                  cursor: r.onClick ? 'pointer' : 'default',
                }}>
                  <div style={{ flex:1, minWidth:0 }}>
                    <div style={{ color:tk.text, fontSize:13.5, fontWeight:500 }}>{r.l}</div>
                    {r.sub && <div style={{ color:tk.text3, fontSize:10.5, marginTop:3, lineHeight:1.4 }}>{r.sub}</div>}
                  </div>
                  {r.r && (
                    <span style={{
                      color: r.accent ? tk.accent : tk.text3, fontSize:12,
                      fontFamily: r.mono ? 'ui-monospace, SF Mono, monospace' : 'inherit',
                      maxWidth:160, overflow:'hidden', textOverflow:'ellipsis', whiteSpace:'nowrap',
                    }}>{r.r}</span>
                  )}
                  {Icon.chevronR(12, tk.text3)}
                </div>
              ))}
            </Card>
          </div>
        ))}
      </div>
    </div>
  );
}

Object.assign(window, { GroupsTab, AuditTab, SettingsTab });
