// screens-init.jsx — first-run bootstrap:
// 1) generate identity  2) scan coord/relay QR  3) review  4) POST pubkey  5) done

// ─────────────────────────────────────────────────────────────
// Shared shell with step indicator
// ─────────────────────────────────────────────────────────────
function InitShell({ tk, step, total, title, sub, onBack, onNext, nextLabel, nextDisabled, nextLoading, children }) {
  return (
    <div style={{ height:'100%', display:'flex', flexDirection:'column' }}>
      {/* nav */}
      <div style={{ padding:'58px 16px 6px', display:'flex', alignItems:'center', justifyContent:'space-between' }}>
        {onBack ? (
          <button onClick={onBack} style={{
            width:36, height:36, borderRadius:18, background:tk.surface,
            border:`0.5px solid ${tk.hairline}`, color:tk.text,
            display:'flex', alignItems:'center', justifyContent:'center', cursor:'pointer',
          }}>{Icon.chevronL(18, tk.text)}</button>
        ) : <div style={{width:36}}/>}
        <div style={{ color:tk.text2, fontSize:12, fontWeight:600, letterSpacing:0.4 }}>
          初始化 · 第 {step} / {total} 步
        </div>
        <div style={{ width:36 }}/>
      </div>

      {/* progress */}
      <div style={{ padding:'4px 22px 0', display:'flex', gap:5 }}>
        {Array.from({ length: total }).map((_, i) => {
          const done = i < step - 1;
          const active = i === step - 1;
          return (
            <div key={i} style={{ flex:1, height:3, borderRadius:2, background:tk.surface2, overflow:'hidden' }}>
              <div style={{
                height:'100%', width: done || active ? '100%' : '0%',
                background: tk.accent,
                boxShadow: active ? `0 0 8px ${tk.accent}80` : 'none',
                transition:'width 0.3s ease',
              }}/>
            </div>
          );
        })}
      </div>

      {/* title */}
      <div style={{ padding:'24px 24px 0' }}>
        <div style={{ color:tk.text, fontSize:24, fontWeight:700, letterSpacing:-0.4, lineHeight:1.2 }}>{title}</div>
        {sub && <div style={{ color:tk.text2, fontSize:13.5, lineHeight:1.55, marginTop:8 }}>{sub}</div>}
      </div>

      {/* body */}
      <div style={{ flex:1, overflow:'auto', padding:'18px 22px 12px' }}>
        {children}
      </div>

      {/* footer */}
      {onNext && (
        <div style={{ padding:'12px 22px 36px' }}>
          <PrimaryBtn tk={tk} onClick={onNext} disabled={nextDisabled || nextLoading}>
            {nextLoading ? (
              <span style={{ display:'inline-flex', alignItems:'center', gap:8 }}>
                <span style={{ width:14, height:14, borderRadius:7,
                  border:`2px solid rgba(10,15,28,0.25)`, borderTopColor:'#0A0F1C',
                  animation:'trineSpin 0.7s linear infinite' }}/>
                请稍候…
              </span>
            ) : (nextLabel || '继续')}
          </PrimaryBtn>
        </div>
      )}
    </div>
  );
}

// ─────────────────────────────────────────────────────────────
// Step 1 — Generate identity
// ─────────────────────────────────────────────────────────────
function InitIdentityStep({ tk, nav }) {
  const [pass, setPass] = React.useState('');
  const [pass2, setPass2] = React.useState('');
  const [generating, setGenerating] = React.useState(false);

  const passOk = pass.length >= 8 && pass === pass2;
  const strength = Math.min(4, Math.floor(pass.length / 4));

  const onGenerate = () => {
    setGenerating(true);
    setTimeout(()=>nav('initScan', { /* identity ready */ }), 1100);
  };

  return (
    <InitShell tk={tk} step={1} total={5}
      title="生成本机身份"
      sub="为这台设备生成一对 secp256k1 身份密钥。私钥由 keystore 口令包装,永不离开设备。"
      onBack={()=>nav('onboarding')}
      onNext={onGenerate}
      nextLabel="生成身份密钥"
      nextDisabled={!passOk}
      nextLoading={generating}
    >
      {/* security note */}
      <div style={{ padding:'12px 14px', borderRadius:12, marginBottom:18,
        background:`${tk.accent}10`, border:`0.5px solid ${tk.accent}33` }}>
        <div style={{ display:'flex', alignItems:'center', gap:7, marginBottom:6 }}>
          {Icon.shield(15, tk.accent)}
          <span style={{ color:tk.accent, fontSize:12, fontWeight:700, letterSpacing:0.3 }}>这把口令很重要</span>
        </div>
        <div style={{ color:tk.text2, fontSize:11.5, lineHeight:1.55 }}>
          所有的 share 和身份私钥都用这把口令派生的 KEK 封装(Argon2id)。<b style={{ color:tk.text }}>口令丢失 = 此设备无法再参与签名</b>,需要让其他守护者帮你 reshare。
        </div>
      </div>

      <SectionLabel tk={tk}>keystore 口令</SectionLabel>
      <div style={{
        padding:'14px 14px', borderRadius:14, marginBottom:8,
        background:tk.surface, border:`0.5px solid ${tk.hairline}`,
      }}>
        <input
          type="password"
          value={pass}
          onChange={e=>setPass(e.target.value)}
          placeholder="至少 8 位 · 推荐 12+"
          style={{
            width:'100%', background:'transparent', border:'none', outline:'none',
            color:tk.text, fontSize:15, fontWeight:500,
            fontFamily:'ui-monospace, SF Mono, monospace', letterSpacing:0.5,
          }}/>
        <div style={{ display:'flex', gap:4, marginTop:10 }}>
          {[0,1,2,3].map(i=>(
            <div key={i} style={{ flex:1, height:3, borderRadius:2,
              background: i < strength ? tk.accent : tk.surface2 }}/>
          ))}
        </div>
      </div>
      <div style={{
        padding:'14px 14px', borderRadius:14, marginBottom:18,
        background:tk.surface, border:`0.5px solid ${pass2 && pass!==pass2 ? tk.danger+'66' : tk.hairline}`,
      }}>
        <input
          type="password"
          value={pass2}
          onChange={e=>setPass2(e.target.value)}
          placeholder="再次输入口令"
          style={{
            width:'100%', background:'transparent', border:'none', outline:'none',
            color:tk.text, fontSize:15, fontWeight:500,
            fontFamily:'ui-monospace, SF Mono, monospace', letterSpacing:0.5,
          }}/>
        {pass2 && pass !== pass2 && (
          <div style={{ color:tk.danger, fontSize:10.5, marginTop:6 }}>两次输入不一致</div>
        )}
      </div>

      <SectionLabel tk={tk}>生物识别</SectionLabel>
      <Card tk={tk} style={{ padding:'12px 14px', display:'flex', alignItems:'center', gap:12 }}>
        <div style={{
          width:36, height:36, borderRadius:11, background:`${tk.accent}14`,
          border:`0.5px solid ${tk.accent}33`, color:tk.accent,
          display:'flex', alignItems:'center', justifyContent:'center',
        }}>
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none">
            <path d="M12 3a8 8 0 00-8 8v3M20 11a8 8 0 00-2.5-5.8M12 7v6c0 2 1 4 3 4M8 12v2c0 3 2 5 5 5M5 17c1 2 3 4 6 4M16 21c2-1 3-4 3-7" stroke={tk.accent} strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round"/>
          </svg>
        </div>
        <div style={{ flex:1 }}>
          <div style={{ color:tk.text, fontSize:13.5, fontWeight:600 }}>Face ID 解锁</div>
          <div style={{ color:tk.text3, fontSize:11, marginTop:2 }}>每次审批 · 启动 · 解封 share 时校验</div>
        </div>
        <div style={{
          width:36, height:22, borderRadius:11, background:tk.accent, position:'relative',
        }}>
          <div style={{ position:'absolute', top:2, right:2, width:18, height:18,
            borderRadius:9, background:'#fff' }}/>
        </div>
      </Card>
    </InitShell>
  );
}

// ─────────────────────────────────────────────────────────────
// Step 2 — Scan QR (camera viewfinder)
// ─────────────────────────────────────────────────────────────
function InitScanStep({ tk, nav }) {
  const [scanning, setScanning] = React.useState(true);
  const [detected, setDetected] = React.useState(false);

  // simulate auto-detect after 3.2s
  React.useEffect(() => {
    if (!scanning) return;
    const t = setTimeout(()=>{ setScanning(false); setDetected(true); }, 3200);
    return ()=>clearTimeout(t);
  }, [scanning]);

  React.useEffect(() => {
    if (detected) {
      const t = setTimeout(()=>nav('initConfirm'), 800);
      return ()=>clearTimeout(t);
    }
  }, [detected]);

  return (
    <InitShell tk={tk} step={2} total={5}
      title="扫描接入二维码"
      sub="向运维要一张接入二维码,包含 coord 端点、relay 多址、一次性 bootstrap token。"
      onBack={()=>nav('initIdentity')}
    >
      {/* Camera viewfinder */}
      <div style={{
        position:'relative', width:'100%', aspectRatio:'1 / 1',
        borderRadius:24, overflow:'hidden', marginBottom:16,
        background:'#000',
      }}>
        {/* fake camera feed: dark gradient + grid texture */}
        <div style={{
          position:'absolute', inset:0,
          background:`
            radial-gradient(80% 60% at 50% 40%, #1a2236 0%, #06070A 70%),
            repeating-linear-gradient(0deg, rgba(255,255,255,0.02) 0 1px, transparent 1px 24px),
            repeating-linear-gradient(90deg, rgba(255,255,255,0.02) 0 1px, transparent 1px 24px)
          `,
        }}/>

        {/* fake QR target */}
        {!detected && (
          <div style={{
            position:'absolute', top:'50%', left:'50%',
            transform:'translate(-50%,-50%)',
            width:170, height:170, opacity:0.6,
          }}>
            <FakeQR color={tk.text2}/>
          </div>
        )}

        {/* viewfinder frame corners */}
        <div style={{
          position:'absolute', top:'50%', left:'50%',
          transform:'translate(-50%,-50%)',
          width:220, height:220, pointerEvents:'none',
        }}>
          {[
            { t:0, l:0,  br:'tl' },
            { t:0, r:0,  br:'tr' },
            { b:0, l:0,  br:'bl' },
            { b:0, r:0,  br:'br' },
          ].map((c, i) => (
            <div key={i} style={{
              position:'absolute', width:34, height:34,
              top:c.t, left:c.l, right:c.r, bottom:c.b,
              borderTop:    c.br.includes('t') ? `2.5px solid ${detected ? tk.accent : '#fff'}` : 'none',
              borderBottom: c.br.includes('b') ? `2.5px solid ${detected ? tk.accent : '#fff'}` : 'none',
              borderLeft:   c.br.includes('l') ? `2.5px solid ${detected ? tk.accent : '#fff'}` : 'none',
              borderRight:  c.br.includes('r') ? `2.5px solid ${detected ? tk.accent : '#fff'}` : 'none',
              borderTopLeftRadius:     c.br==='tl' ? 10 : 0,
              borderTopRightRadius:    c.br==='tr' ? 10 : 0,
              borderBottomLeftRadius:  c.br==='bl' ? 10 : 0,
              borderBottomRightRadius: c.br==='br' ? 10 : 0,
              transition:'border-color 0.3s',
            }}/>
          ))}

          {/* scan line */}
          {scanning && (
            <div style={{
              position:'absolute', left:6, right:6, height:2,
              background:`linear-gradient(90deg, transparent, ${tk.accent}, transparent)`,
              boxShadow:`0 0 8px ${tk.accent}`,
              animation:'trineScan 2.2s ease-in-out infinite',
            }}/>
          )}

          {/* detected check */}
          {detected && (
            <div style={{
              position:'absolute', inset:0,
              display:'flex', alignItems:'center', justifyContent:'center',
            }}>
              <div style={{
                width:64, height:64, borderRadius:32,
                background:`${tk.accent}`,
                display:'flex', alignItems:'center', justifyContent:'center',
                boxShadow:`0 0 24px ${tk.accent}aa`,
                animation:'trinePop 0.4s ease-out',
              }}>
                {Icon.check(36, '#0A0F1C')}
              </div>
            </div>
          )}
        </div>

        {/* hint */}
        <div style={{
          position:'absolute', bottom:14, left:0, right:0, textAlign:'center',
          color: detected ? tk.accent : 'rgba(255,255,255,0.6)',
          fontSize:11.5, fontWeight:600, letterSpacing:0.5,
        }}>
          {detected ? '✓ 已识别 · 解析中…' : '将二维码置于框内'}
        </div>
      </div>

      <div style={{ display:'flex', gap:10 }}>
        <GhostBtn tk={tk} onClick={()=>{}} style={{ flex:1, height:46, fontSize:13 }}>
          <span style={{ display:'inline-flex', alignItems:'center', gap:6 }}>
            {Icon.qr(15, tk.text)}从相册选择
          </span>
        </GhostBtn>
        <GhostBtn tk={tk} onClick={()=>nav('initConfirm')} style={{ flex:1, height:46, fontSize:13 }}>
          手动输入
        </GhostBtn>
      </div>

      <div style={{ marginTop:14, padding:'10px 12px',
        background:tk.surface, borderRadius:11,
        border:`0.5px solid ${tk.hairline}`,
        display:'flex', alignItems:'flex-start', gap:8,
      }}>
        {Icon.info(14, tk.text3)}
        <div style={{ flex:1, color:tk.text3, fontSize:10.5, lineHeight:1.5 }}>
          QR 中的 bootstrap token 为一次性凭证(默认 10 分钟过期),与本机身份私钥绑定后失效。
        </div>
      </div>
    </InitShell>
  );
}

// ─────────────────────────────────────────────────────────────
// Step 3 — Confirm endpoints
// ─────────────────────────────────────────────────────────────
function InitConfirmStep({ tk, nav }) {
  const parsed = {
    coord:    'https://coord.zzci.io',
    tls:      'mTLS · pinned cert SHA256: 8a4f…',
    relayPID: '12D3KooW7Q2x…NfP9',
    relayAddrs: [
      '/dns4/relay.zzci.io/tcp/4001/wss',
      '/ip4/10.0.1.5/tcp/4001',
    ],
    groupHint:'grp_5a8e7b3c · 财库主组',
    bootstrapToken: 'btk_2J9XaQ…wKf3 (一次性)',
    expires:  '2026-05-21 15:00 · 8 分钟后',
  };

  return (
    <InitShell tk={tk} step={3} total={5}
      title="确认接入端点"
      sub="确认下列信息来源可信。一旦下一步注册,身份公钥即上传给该 coord。"
      onBack={()=>nav('initScan')}
      onNext={()=>nav('initRegister')}
      nextLabel="确认并注册到 coord"
    >
      <SectionLabel tk={tk}>coord 服务器</SectionLabel>
      <Card tk={tk} style={{ padding:'2px 14px', marginBottom:14 }}>
        <KV tk={tk} k="HTTP 端点"  v={parsed.coord} mono accent/>
        <div style={{ borderTop:`0.5px solid ${tk.hairline}` }}/>
        <KV tk={tk} k="鉴权"       v="mTLS + 客户端证书" sub={parsed.tls} mono/>
      </Card>

      <SectionLabel tk={tk}>relay 网络</SectionLabel>
      <Card tk={tk} style={{ padding:'2px 14px', marginBottom:14 }}>
        <KV tk={tk} k="peerID"    v={parsed.relayPID} mono/>
        <div style={{ borderTop:`0.5px solid ${tk.hairline}` }}/>
        <div style={{ padding:'10px 0' }}>
          <div style={{ color:tk.text2, fontSize:12.5, marginBottom:8 }}>multiaddrs</div>
          {parsed.relayAddrs.map((a,i)=>(
            <div key={i} style={{
              padding:'7px 10px', borderRadius:8, marginBottom: i<parsed.relayAddrs.length-1 ? 5 : 0,
              background:tk.surface2,
              color:tk.text, fontSize:11.5,
              fontFamily:'ui-monospace, SF Mono, monospace', letterSpacing:0.2,
              wordBreak:'break-all',
            }}>{a}</div>
          ))}
        </div>
      </Card>

      <SectionLabel tk={tk}>bootstrap</SectionLabel>
      <Card tk={tk} style={{ padding:'2px 14px', marginBottom:14 }}>
        <KV tk={tk} k="一次性 token" v={parsed.bootstrapToken} mono/>
        <div style={{ borderTop:`0.5px solid ${tk.hairline}` }}/>
        <KV tk={tk} k="过期时间"    v={parsed.expires}/>
        <div style={{ borderTop:`0.5px solid ${tk.hairline}` }}/>
        <KV tk={tk} k="目标钱包"    v={parsed.groupHint} sub="注册成功后接收 START dispatch"/>
      </Card>

      <div style={{ padding:'12px 14px', borderRadius:12,
        background:`${tk.warn}10`, border:`0.5px solid ${tk.warn}40`,
        display:'flex', alignItems:'flex-start', gap:8,
      }}>
        {Icon.warn(15, tk.warn)}
        <div style={{ flex:1, color:tk.text2, fontSize:11.5, lineHeight:1.55 }}>
          <b style={{ color:tk.warn }}>请向你的运维方核对</b> coord 域名与证书 SHA256 — 接入到错误的 coord 会让攻击者拿到你的身份公钥(无法签名但能消耗配额)。
        </div>
      </div>
    </InitShell>
  );
}

// ─────────────────────────────────────────────────────────────
// Step 4 — POST identity pubkey to coord
// ─────────────────────────────────────────────────────────────
function InitRegisterStep({ tk, nav }) {
  // stages: derive → post → coord verify → assign → done
  const [stage, setStage] = React.useState(0);
  React.useEffect(() => {
    if (stage < 4) {
      const t = setTimeout(()=>setStage(stage+1), [700, 900, 1100, 800][stage]);
      return ()=>clearTimeout(t);
    } else {
      const t = setTimeout(()=>nav('initDone'), 600);
      return ()=>clearTimeout(t);
    }
  }, [stage]);

  const stages = [
    { label:'派生身份公钥',     sub:'secp256k1 · 0x04a2c9f1...8e3b' },
    { label:'POST /v1/members/enroll', sub:'Authorization: Bearer btk_2J9XaQ…' },
    { label:'coord 验签 + 校验 bootstrap', sub:'expected_members 添加 · 落 sqlite' },
    { label:'分配 memberId · 同步 group config', sub:'memberId = m0 · partyIndex = 0' },
  ];

  return (
    <InitShell tk={tk} step={4} total={5}
      title="向 coord 注册公钥"
      sub="把本机身份公钥上传到 coord。私钥永不离开设备。"
      // no Back — registration in progress is hard to abort
    >
      {/* request preview */}
      <Card tk={tk} style={{ padding:'14px 14px', marginBottom:18 }}>
        <div style={{ color:tk.text3, fontSize:10.5, fontWeight:600, letterSpacing:0.6,
          textTransform:'uppercase', marginBottom:8 }}>HTTP 请求</div>
        <div style={{
          color:tk.text2, fontSize:11.5,
          fontFamily:'ui-monospace, SF Mono, monospace',
          lineHeight:1.6, letterSpacing:0.2,
        }}>
          <div><span style={{ color:tk.accent }}>POST</span> {'https://coord.zzci.io'}<span style={{ color:tk.text }}>/v1/members/enroll</span></div>
          <div style={{ color:tk.text3 }}>Authorization: Bearer btk_2J9XaQ…wKf3</div>
          <div style={{ color:tk.text3 }}>X-Member-Ts: 1747843200000</div>
          <div style={{ color:tk.text3 }}>X-Member-Sig: 0x8a4f…32 (self-sign)</div>
          <div style={{ marginTop:6 }}>{'{'}</div>
          <div>{'  '}<span style={{ color:tk.text3 }}>"identityPub":</span>{' '}<span style={{ color:tk.accent }}>"0x04a2c9f1…8e3b"</span>,</div>
          <div>{'  '}<span style={{ color:tk.text3 }}>"deviceLabel":</span>{' '}<span style={{ color:tk.accent }}>"iPhone 15 Pro"</span>,</div>
          <div>{'  '}<span style={{ color:tk.text3 }}>"groupHint":</span>{' '}<span style={{ color:tk.accent }}>"grp_5a8e7b3c"</span>{'\n'}</div>
          <div>{'}'}</div>
        </div>
      </Card>

      {/* progress steps */}
      <div style={{ display:'flex', flexDirection:'column', gap:8 }}>
        {stages.map((st, i) => {
          const done = i < stage;
          const active = i === stage;
          return (
            <div key={i} style={{
              display:'flex', alignItems:'center', gap:12,
              padding:'11px 12px', borderRadius:12,
              background: active ? `${tk.accent}10` : tk.surface,
              border:`0.5px solid ${active ? tk.accent+'55' : tk.hairline}`,
              opacity: done || active ? 1 : 0.5,
              transition:'all 0.3s ease',
            }}>
              <div style={{
                width:26, height:26, borderRadius:13, flexShrink:0,
                background: done ? tk.accent : active ? `${tk.accent}22` : tk.surface2,
                border: active ? `1.5px solid ${tk.accent}` : `0.5px solid ${tk.hairline}`,
                display:'flex', alignItems:'center', justifyContent:'center',
                color: done ? '#0A0F1C' : tk.accent,
                fontSize:11, fontWeight:700,
                position:'relative',
              }}>
                {done ? Icon.check(13, '#0A0F1C') : (
                  <>
                    {active && (
                      <div style={{
                        position:'absolute', inset:-3, borderRadius:17,
                        border:`1.5px solid ${tk.accent}`, opacity:0.4,
                        animation:'trinePulse 1.4s ease-in-out infinite',
                      }}/>
                    )}
                    {i+1}
                  </>
                )}
              </div>
              <div style={{ flex:1, minWidth:0 }}>
                <div style={{ color: active || done ? tk.text : tk.text3,
                  fontSize:12.5, fontWeight:600 }}>{st.label}</div>
                <div style={{ color:tk.text3, fontSize:10, marginTop:2,
                  fontFamily:'ui-monospace, SF Mono, monospace',
                  overflow:'hidden', textOverflow:'ellipsis', whiteSpace:'nowrap',
                }}>{st.sub}</div>
              </div>
              {active && (
                <div style={{ width:14, height:14, borderRadius:7,
                  border:`2px solid ${tk.accent}33`, borderTopColor:tk.accent,
                  animation:'trineSpin 0.7s linear infinite' }}/>
              )}
            </div>
          );
        })}
      </div>
    </InitShell>
  );
}

// ─────────────────────────────────────────────────────────────
// Step 5 — Done
// ─────────────────────────────────────────────────────────────
function InitDoneStep({ tk, nav }) {
  return (
    <div style={{ height:'100%', display:'flex', flexDirection:'column' }}>
      <div style={{ padding:'58px 22px 0', textAlign:'right' }}>
        <span style={{ color:tk.text3, fontSize:11, fontWeight:600 }}>初始化 · 第 5 / 5 步</span>
      </div>
      <div style={{ padding:'4px 22px 0', display:'flex', gap:5 }}>
        {Array.from({ length: 5 }).map((_, i) => (
          <div key={i} style={{ flex:1, height:3, borderRadius:2, background:tk.accent,
            boxShadow: i===4 ? `0 0 8px ${tk.accent}80` : 'none' }}/>
        ))}
      </div>

      <div style={{ flex:1, padding:'0 26px', display:'flex', flexDirection:'column',
        justifyContent:'center', alignItems:'center' }}>
        <div style={{
          width:104, height:104, borderRadius:52,
          background:`radial-gradient(circle, ${tk.accent}44, transparent 70%)`,
          display:'flex', alignItems:'center', justifyContent:'center',
        }}>
          <TrineMark color={tk.accent} size={84} glow/>
        </div>

        <div style={{ color:tk.text, fontSize:26, fontWeight:700, marginTop:24,
          letterSpacing:-0.5, textAlign:'center' }}>已接入 coord</div>
        <div style={{ color:tk.text2, fontSize:13.5, marginTop:6, textAlign:'center', lineHeight:1.55 }}>
          公钥已注册,身份私钥安全留在本机。<br/>
          后续请等待 keygen START dispatch。
        </div>

        <Card tk={tk} style={{ width:'100%', marginTop:26, padding:'2px 14px' }}>
          <KV tk={tk} k="memberId"      v="m0" mono accent/>
          <div style={{ borderTop:`0.5px solid ${tk.hairline}` }}/>
          <KV tk={tk} k="partyIndex"    v="0"/>
          <div style={{ borderTop:`0.5px solid ${tk.hairline}` }}/>
          <KV tk={tk} k="coord"         v="coord.zzci.io" mono sub="mTLS · 38ms"/>
          <div style={{ borderTop:`0.5px solid ${tk.hairline}` }}/>
          <KV tk={tk} k="relay"         v="12D3KooW…7Q2x" mono sub="已建立 libp2p stream"/>
        </Card>
      </div>

      <div style={{ padding:'14px 22px 38px' }}>
        <PrimaryBtn tk={tk} onClick={()=>nav('inbox')}>进入收件箱</PrimaryBtn>
      </div>
    </div>
  );
}

// ─────────────────────────────────────────────────────────────
// FakeQR — decorative QR code SVG (purely visual)
// ─────────────────────────────────────────────────────────────
function FakeQR({ color }) {
  // deterministic 21x21 grid with finder patterns
  const N = 21;
  const cells = [];
  // PRNG seeded for stable pattern
  let s = 12345;
  const rand = () => { s = (s * 1664525 + 1013904223) >>> 0; return (s & 0xffff) / 0x10000; };
  for (let y=0; y<N; y++) {
    for (let x=0; x<N; x++) {
      // finder patterns in 3 corners
      const inFinder = (
        (x < 7 && y < 7) ||
        (x >= N-7 && y < 7) ||
        (x < 7 && y >= N-7)
      );
      if (inFinder) {
        const fx = x < 7 ? x : x - (N-7);
        const fy = y < 7 ? y : y - (N-7);
        const fill = (fx===0||fx===6||fy===0||fy===6) || (fx>=2 && fx<=4 && fy>=2 && fy<=4);
        if (fill) cells.push([x,y]);
      } else {
        if (rand() > 0.55) cells.push([x,y]);
      }
    }
  }
  return (
    <svg viewBox={`0 0 ${N} ${N}`} width="100%" height="100%" shapeRendering="crispEdges">
      {cells.map(([x,y], i) => <rect key={i} x={x} y={y} width="1" height="1" fill={color}/>)}
    </svg>
  );
}

Object.assign(window, {
  InitShell, InitIdentityStep, InitScanStep, InitConfirmStep, InitRegisterStep, InitDoneStep, FakeQR,
});
