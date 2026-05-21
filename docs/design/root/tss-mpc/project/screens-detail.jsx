// screens-detail.jsx — WYSIWYS pending detail + signing + result

// ─────────────────────────────────────────────────────────────
// Tiny components used here
// ─────────────────────────────────────────────────────────────
function KV({ tk, k, v, mono, accent, sub, danger }) {
  return (
    <div style={{ display:'flex', justifyContent:'space-between', alignItems:'flex-start', gap:14, padding:'10px 0' }}>
      <span style={{ color:tk.text2, fontSize:12.5, lineHeight:1.4 }}>{k}</span>
      <div style={{ textAlign:'right', maxWidth:230 }}>
        <div style={{
          color: danger ? tk.danger : accent ? tk.accent : tk.text,
          fontSize: 13, fontWeight: 600,
          fontFamily: mono ? 'ui-monospace, SF Mono, monospace' : 'inherit',
          wordBreak: 'break-all', lineHeight:1.45,
        }}>{v}</div>
        {sub && <div style={{ color:tk.text3, fontSize:10.5, marginTop:2 }}>{sub}</div>}
      </div>
    </div>
  );
}

function SectionLabel({ tk, children, right }) {
  return (
    <div style={{ display:'flex', justifyContent:'space-between', alignItems:'baseline',
      margin:'0 4px 8px', color:tk.text2, fontSize:11, fontWeight:600,
      letterSpacing:0.6, textTransform:'uppercase' }}>
      <span>{children}</span>
      {right}
    </div>
  );
}

// ─────────────────────────────────────────────────────────────
// PendingDetail — WYSIWYS. The most important screen in the app.
// ─────────────────────────────────────────────────────────────
function PendingDetailScreen({ tk, nav, envelope }) {
  if (!envelope) return null;
  const env = envelope;
  const group = GROUPS.find(g=>g.groupId === env.groupId);
  const myDec = env.decisions.find(d=>d.self);
  const approvedCount = env.decisions.filter(d=>d.state==='approved').length;
  const isMismatch = !env.crossCheckOk;
  const tx = env.unsignedTxSummary;

  return (
    <div style={{ height:'100%', display:'flex', flexDirection:'column' }}>
      <TopBar tk={tk} title="签名审批 (WYSIWYS)" onBack={()=>nav('inbox')} right={
        <button style={{ width:36, height:36, borderRadius:18, background:tk.surface, border:`0.5px solid ${tk.hairline}`, color:tk.text, cursor:'pointer', display:'flex', alignItems:'center', justifyContent:'center' }}>{Icon.more(18, tk.text)}</button>
      }/>

      <div style={{ flex:1, overflow:'auto', padding:'8px 18px 0' }}>
        {/* Top status row */}
        <div style={{ display:'flex', alignItems:'center', gap:8, marginBottom:14 }}>
          <ChainBadge tk={tk} chain={env.chain} label={env.chainLabel}/>
          <div style={{
            padding:'3px 8px', borderRadius:6,
            background:`${tk.warn}1f`, color:tk.warn,
            fontSize:10.5, fontWeight:700, letterSpacing:0.4,
          }}>{env.status}</div>
          <div style={{ flex:1 }}/>
          <div style={{ display:'flex', alignItems:'center', gap:4, color:tk.warn, fontSize:11.5, fontWeight:600 }}>
            {Icon.clock(13, tk.warn)}
            <span>过期 {fmtExpiry(env.expiresIn)}</span>
          </div>
        </div>

        {/* Mismatch banner — top priority */}
        {isMismatch && (
          <div style={{
            padding:'12px 14px', borderRadius:14, marginBottom:14,
            background:`linear-gradient(180deg, ${tk.danger}20, ${tk.danger}08)`,
            border:`0.5px solid ${tk.danger}66`,
          }}>
            <div style={{ display:'flex', alignItems:'center', gap:8, marginBottom:6 }}>
              {Icon.warn(18, tk.danger)}
              <span style={{ color:tk.danger, fontSize:13.5, fontWeight:700 }}>WYSIWYS 检查未通过</span>
            </div>
            <div style={{ color:tk.text, fontSize:12, lineHeight:1.5 }}>
              {env.mismatchHint}。<br/>
              <span style={{ color:tk.text2 }}>提议者签名 (A 面) 与 businessInfo (B 面) 哈希不一致 — <b style={{ color:tk.danger }}>不要批准</b>,直接拒绝并联系提议方。</span>
            </div>
          </div>
        )}

        {/* Hero amount */}
        <div style={{
          padding:'18px 16px', borderRadius:18, marginBottom:16,
          background:`linear-gradient(180deg, ${tk.accent}10, transparent)`,
          border:`0.5px solid ${tk.accent}33`,
          textAlign:'center',
        }}>
          <div style={{ color:tk.text2, fontSize:11.5, letterSpacing:0.5 }}>申请签名</div>
          <div style={{ display:'flex', alignItems:'baseline', justifyContent:'center', gap:8, marginTop:6 }}>
            <span style={{ color:tk.text, fontSize:34, fontWeight:700, letterSpacing:-0.7, fontVariantNumeric:'tabular-nums' }}>
              {tx.value}
            </span>
          </div>
          <div style={{ color:tk.text3, fontSize:12, marginTop:3 }}>{tx.valueFiat}</div>
          <div style={{ marginTop:14, padding:'8px 12px', borderRadius:10,
            background:'rgba(0,0,0,0.25)', border:`0.5px solid ${tk.hairline}`,
            color:tk.text, fontSize:13, fontWeight:600 }}>
            {env.businessInfo.title}
          </div>
        </div>

        {/* A facts (from on-chain decoded) */}
        <SectionLabel tk={tk} right={
          <span style={{ color: env.crossCheckOk ? tk.accent : tk.danger, fontSize:10, fontWeight:700, letterSpacing:0.5 }}>
            {env.crossCheckOk ? '✓ A↔B 一致' : '✗ A↔B 不一致'}
          </span>
        }>A 面 · 链上事实 (设备解码)</SectionLabel>
        <Card tk={tk} style={{ padding:'2px 14px', marginBottom:14 }}>
          <KV tk={tk} k="目标" v={tx.to} mono sub={tx.toLabel} danger={tx.toLabel.startsWith('⚠')}/>
          <div style={{ borderTop:`0.5px solid ${tk.hairline}` }}/>
          <KV tk={tk} k="金额"   v={tx.value}     sub={tx.valueFiat}/>
          <div style={{ borderTop:`0.5px solid ${tk.hairline}` }}/>
          <KV tk={tk} k="网络"   v={env.chainLabel} sub={env.chain} mono/>
          <div style={{ borderTop:`0.5px solid ${tk.hairline}` }}/>
          <KV tk={tk} k="nonce / gas" v={`${tx.nonce} · ${tx.gasPrice}`} mono sub={`gasLimit ${tx.gasLimit}`}/>
          <div style={{ borderTop:`0.5px solid ${tk.hairline}` }}/>
          <KV tk={tk} k="calldata" v={tx.data.length > 12 ? tx.data.slice(0,12)+'…' : tx.data} mono sub={tx.data === '0x' ? 'EOA 转账' : '合约调用'}/>
        </Card>

        {/* B info (businessInfo + metaHash) */}
        <SectionLabel tk={tk}>B 面 · 业务说明 (proposer)</SectionLabel>
        <Card tk={tk} style={{ padding:'2px 14px', marginBottom:14 }}>
          <KV tk={tk} k="标题" v={env.businessInfo.title}/>
          <div style={{ borderTop:`0.5px solid ${tk.hairline}` }}/>
          <KV tk={tk} k="工单号" v={env.businessInfo.orderId} mono/>
          <div style={{ borderTop:`0.5px solid ${tk.hairline}` }}/>
          <KV tk={tk} k="操作员" v={env.businessInfo.operator} mono/>
          <div style={{ borderTop:`0.5px solid ${tk.hairline}` }}/>
          <KV tk={tk} k="备注" v={env.businessInfo.memo}/>
          <div style={{ borderTop:`0.5px solid ${tk.hairline}` }}/>
          <KV tk={tk}
            k="metaHash 校验"
            v={env.metaHashOk ? '✓ 通过' : '✗ 失败'}
            accent={env.metaHashOk}
            danger={!env.metaHashOk}
            sub={env.metaHashOk ? 'H(businessInfo) 与 proposerSig 中 metaHash 一致' : '哈希不匹配,businessInfo 可能被篡改'}/>
        </Card>

        {/* Digest32 + envelope meta */}
        <SectionLabel tk={tk}>签名摘要 · digest32</SectionLabel>
        <Card tk={tk} style={{ padding:'14px 14px', marginBottom:14 }}>
          <div style={{
            padding:'10px 12px', borderRadius:10, background:tk.surface2,
            color:tk.accent, fontSize:11.5,
            fontFamily:'ui-monospace, SF Mono, monospace',
            letterSpacing:0.4, wordBreak:'break-all', lineHeight:1.5,
            border:`0.5px solid ${tk.accent}22`,
          }}>{env.digest32}</div>
          <div style={{ display:'flex', justifyContent:'space-between', alignItems:'center', marginTop:10 }}>
            <span style={{ color:tk.text3, fontSize:11 }}>设备将本地重算并断言 == digest32</span>
            <div style={{ display:'flex', alignItems:'center', gap:4, color:tk.accent, fontSize:11, fontWeight:600 }}>
              {Icon.shield(12, tk.accent)} 本地重算
            </div>
          </div>
        </Card>

        {/* Envelope meta */}
        <SectionLabel tk={tk}>信封元数据</SectionLabel>
        <Card tk={tk} style={{ padding:'2px 14px', marginBottom:14 }}>
          <KV tk={tk} k="requestId"  v={env.requestId} mono/>
          <div style={{ borderTop:`0.5px solid ${tk.hairline}` }}/>
          <KV tk={tk} k="提议者"      v={env.proposerLabel} sub={env.proposer} mono/>
          <div style={{ borderTop:`0.5px solid ${tk.hairline}` }}/>
          <KV tk={tk} k="钱包"        v={group.moniker} sub={group.groupId} mono/>
          <div style={{ borderTop:`0.5px solid ${tk.hairline}` }}/>
          <KV tk={tk} k="收到时间"    v={env.receivedAt} sub={'本设备 B6 dispatch'}/>
        </Card>

        {/* Other members' decisions */}
        <SectionLabel tk={tk} right={
          <span style={{ color:tk.accent, fontSize:10, fontWeight:700, letterSpacing:0.5 }}>
            {approvedCount} / {group.threshold} 已批
          </span>
        }>其他守护者</SectionLabel>
        <Card tk={tk} style={{ padding:'12px 14px', marginBottom:18 }}>
          {env.decisions.map((d,i)=>{
            const m = group.members.find(mm=>mm.id===d.id);
            const cfg = d.state === 'approved'
              ? { c:tk.accent, label:'已批准' }
              : d.state === 'rejected'
              ? { c:tk.danger, label:'已拒绝' }
              : { c:tk.text3,  label:'等待中' };
            return (
              <div key={d.id} style={{
                display:'flex', alignItems:'center', gap:11,
                padding:'9px 0',
                borderTop: i>0 ? `0.5px solid ${tk.hairline}` : 'none',
              }}>
                <div style={{
                  width:30, height:30, borderRadius:9,
                  background: d.self ? `${tk.accent}1a` : tk.surface2,
                  border:`0.5px solid ${d.self ? tk.accent+'40' : tk.hairline}`,
                  display:'flex', alignItems:'center', justifyContent:'center',
                  color: d.self ? tk.accent : tk.text2,
                  fontSize:11, fontWeight:700, letterSpacing:0.2,
                  fontFamily:'ui-monospace, SF Mono, monospace',
                }}>{d.id}</div>
                <div style={{ flex:1 }}>
                  <div style={{ color:tk.text, fontSize:13, fontWeight:500 }}>
                    {m?.label} {d.self && <span style={{ color:tk.accent, fontWeight:600 }}>· 本机</span>}
                  </div>
                  <div style={{ color:tk.text3, fontSize:10.5, marginTop:1 }}>
                    {d.state === 'pending' ? '等待决策' : `${cfg.label} · ${d.state==='approved'?'刚刚':'4 分钟前'}`}
                  </div>
                </div>
                <div style={{ color:cfg.c, fontSize:11, fontWeight:700 }}>
                  {d.state === 'approved' && Icon.check(16, tk.accent)}
                  {d.state === 'rejected' && Icon.close(16, tk.danger)}
                  {d.state === 'pending' && (
                    <div style={{ width:14, height:14, borderRadius:7, border:`1.5px solid ${tk.text3}` }}/>
                  )}
                </div>
              </div>
            );
          })}
        </Card>

        <div style={{ height:14 }}/>
      </div>

      {/* Sticky action footer */}
      <div style={{ padding:'12px 18px 32px',
        background: `linear-gradient(180deg, transparent 0%, ${tk.bg} 25%)`,
        borderTop:`0.5px solid ${tk.hairline}`,
      }}>
        {myDec.state === 'pending' ? (
          <>
            <div style={{ display:'flex', alignItems:'center', gap:5, justifyContent:'center',
              color:tk.text3, fontSize:10.5, marginBottom:10 }}>
              {Icon.shield(11, tk.text3)}
              <span>按下批准 → 触发 Face ID → 本地 sign session 启动</span>
            </div>
            <div style={{ display:'flex', gap:10 }}>
              <GhostBtn tk={tk} onClick={()=>nav('inbox')} style={{ flex:1 }}>
                <span style={{ color: isMismatch ? tk.danger : tk.text }}>{isMismatch ? '拒绝(推荐)' : '拒绝'}</span>
              </GhostBtn>
              <PrimaryBtn tk={tk}
                onClick={()=>!isMismatch && nav('signing', env)}
                disabled={isMismatch}
                style={{ flex:1.4 }}>
                {isMismatch ? '不可批准' : '批准 · Approve'}
              </PrimaryBtn>
            </div>
          </>
        ) : (
          <PrimaryBtn tk={tk} onClick={()=>nav('inbox')}>返回收件箱</PrimaryBtn>
        )}
      </div>
    </div>
  );
}

// ─────────────────────────────────────────────────────────────
// Signing screen — runs MPC rounds locally
// ─────────────────────────────────────────────────────────────
function SigningScreen({ tk, nav, vizVariant='orbit', envelope }) {
  const [phase] = useSigningPhase(true, 5400);
  const round = phase < 0.34 ? 1 : phase < 0.67 ? 2 : 3;
  const rounds = [
    { label:'承诺值广播',     sub:'Round 1 · commit · 各方播 (γᵢ, gᵅᵢ)' },
    { label:'分片交换',       sub:'Round 2 · share · 加密 MtA 双方 OT' },
    { label:'聚合 σ',         sub:'Round 3 · combine · sᵢ → σ = (r, s)' },
  ];
  React.useEffect(() => {
    if (phase >= 1) {
      const t = setTimeout(() => nav('result', envelope), 700);
      return () => clearTimeout(t);
    }
  }, [phase]);

  return (
    <div style={{ height:'100%', display:'flex', flexDirection:'column', position:'relative' }}>
      <div style={{
        position:'absolute', inset:0, pointerEvents:'none',
        background:`radial-gradient(60% 50% at 50% 38%, ${tk.accent}1c, transparent 70%)`,
      }}/>

      <TopBar tk={tk} title="签名进行中"/>

      <div style={{ flex:1, display:'flex', flexDirection:'column', justifyContent:'center', padding:'0 24px', position:'relative' }}>
        <div style={{ textAlign:'center', marginBottom:6 }}>
          <div style={{
            display:'inline-flex', alignItems:'center', gap:8,
            padding:'5px 11px', borderRadius:99,
            background: `${tk.accent}14`, color:tk.accent,
            fontSize:11, fontWeight:600, letterSpacing:1, textTransform:'uppercase',
          }}>
            <span style={{
              width:6, height:6, borderRadius:3, background:tk.accent,
              animation:'trinePulse 1.2s ease-in-out infinite',
            }}/>
            ECDSA · {envelope ? envelope.chainLabel : '—'}
          </div>
        </div>

        <SigningViz accent={tk.accent} variant={vizVariant} phase={phase} size={296}/>

        <div style={{ marginTop:10, textAlign:'center' }}>
          <div style={{ color:tk.text, fontSize:20, fontWeight:600, letterSpacing:-0.3 }}>
            {round === 3 && phase > 0.95 ? '签名完成' : rounds[round-1].label}
          </div>
          <div style={{ color:tk.text2, fontSize:12, marginTop:5, fontFamily:'ui-monospace, SF Mono, monospace', letterSpacing:0.2 }}>
            {rounds[round-1].sub}
          </div>
        </div>

        {/* round track */}
        <div style={{ display:'flex', gap:6, justifyContent:'center', marginTop:18, padding:'0 8px' }}>
          {[1,2,3].map(n => {
            const active = n === round;
            const localT = active ? Math.max(0, Math.min(1, (phase - (n-1)*0.333) / 0.333))
                          : (n < round ? 1 : 0);
            return (
              <div key={n} style={{ flex:1, height:3, borderRadius:2, background:tk.surface2, overflow:'hidden', position:'relative' }}>
                <div style={{
                  position:'absolute', inset:0, width:`${localT*100}%`,
                  background:tk.accent, transition:'width .1s linear',
                  boxShadow: `0 0 8px ${tk.accent}80`,
                }}/>
              </div>
            );
          })}
        </div>

        <div style={{ textAlign:'center', color:tk.text3, fontSize:10.5, marginTop:14,
          fontFamily:'ui-monospace, SF Mono, monospace' }}>
          {round===1 && '· m0 → broadcasting kᵢ commit ·'}
          {round===2 && '· MtA × 1 with m1 (lone signer absent) ·'}
          {round===3 && (phase > 0.95 ? '· σ verified · POST /v1/requests/{id}/result ·' : '· combining partial sᵢ ·')}
        </div>
      </div>

      <div style={{ padding:'14px 22px 36px' }}>
        <GhostBtn tk={tk} onClick={()=>nav('inbox')} style={{ width:'100%' }}>取消会话</GhostBtn>
      </div>
    </div>
  );
}

// ─────────────────────────────────────────────────────────────
// Result screen
// ─────────────────────────────────────────────────────────────
function ResultScreen({ tk, nav, envelope }) {
  return (
    <div style={{ height:'100%', display:'flex', flexDirection:'column' }}>
      <div style={{ flex:1, padding:'0 24px', display:'flex', flexDirection:'column',
        justifyContent:'center', alignItems:'center' }}>
        <div style={{
          width:96, height:96, borderRadius:48,
          background:`radial-gradient(circle, ${tk.accent}33, transparent 70%)`,
          display:'flex', alignItems:'center', justifyContent:'center',
        }}>
          <div style={{
            width:70, height:70, borderRadius:35,
            border:`1.5px solid ${tk.accent}`,
            background:`${tk.accent}1c`,
            display:'flex', alignItems:'center', justifyContent:'center',
            boxShadow:`0 0 32px ${tk.accent}55`,
          }}>{Icon.check(34, tk.accent)}</div>
        </div>

        <div style={{ color:tk.text, fontSize:24, fontWeight:700, marginTop:22,
          letterSpacing:-0.4, textAlign:'center' }}>
          σ 已生成 · 已上报
        </div>
        <div style={{ color:tk.text2, fontSize:13, marginTop:6, textAlign:'center', lineHeight:1.5 }}>
          POST /v1/requests/{envelope?.requestId}/result 200<br/>
          coord 已验签 → RETURNED → A4 webhook 已触发
        </div>

        <Card tk={tk} style={{ width:'100%', marginTop:26, padding:'14px 16px' }}>
          <div style={{ display:'flex', justifyContent:'space-between', alignItems:'center', paddingBottom:11, borderBottom:`0.5px solid ${tk.hairline}` }}>
            <span style={{ color:tk.text2, fontSize:12 }}>RSV (65 bytes)</span>
            <div style={{ color:tk.text3 }}>{Icon.copy(13, tk.text3)}</div>
          </div>
          <div style={{ padding:'10px 0',
            color:tk.accent, fontSize:11,
            fontFamily:'ui-monospace, SF Mono, monospace',
            letterSpacing:0.3, wordBreak:'break-all', lineHeight:1.5,
          }}>0x8a4f2c39e1b7d04f3c2a99c1...e6f02839472d1ab1c · v=27</div>
          <div style={{ display:'flex', justifyContent:'space-between', alignItems:'center', paddingTop:11,
            borderTop:`0.5px solid ${tk.hairline}` }}>
            <span style={{ color:tk.text2, fontSize:12 }}>本次参与</span>
            <div style={{ display:'flex', gap:4 }}>
              <span style={{ padding:'2px 7px', borderRadius:5, background:`${tk.accent}1a`,
                color:tk.accent, fontSize:10.5, fontWeight:700 }}>m0</span>
              <span style={{ padding:'2px 7px', borderRadius:5, background:`${tk.accent}1a`,
                color:tk.accent, fontSize:10.5, fontWeight:700 }}>m1</span>
              <span style={{ padding:'2px 7px', borderRadius:5, background:tk.surface2,
                color:tk.text3, fontSize:10.5, fontWeight:700 }}>m2</span>
            </div>
          </div>
        </Card>
      </div>

      <div style={{ padding:'14px 22px 38px' }}>
        <PrimaryBtn tk={tk} onClick={()=>nav('inbox')}>返回收件箱</PrimaryBtn>
      </div>
    </div>
  );
}

Object.assign(window, { PendingDetailScreen, SigningScreen, ResultScreen, KV, SectionLabel });
