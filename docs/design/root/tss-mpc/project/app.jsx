// app.jsx — Trine Signer: B-side TSS member client

const TWEAK_DEFAULTS = /*EDITMODE-BEGIN*/{
  "theme": "midnight",
  "accent": "#5EEAD4",
  "vizVariant": "orbit",
  "showFrame": true,
  "startScreen": "inbox"
}/*EDITMODE-END*/;

function App() {
  const [t, setTweak] = useTweaks(TWEAK_DEFAULTS);
  const tk = makeTokens(t);

  const [screen, setScreen] = React.useState(t.startScreen || 'inbox');
  const [tab, setTab]       = React.useState('inbox');
  const [payload, setPayload] = React.useState(null);

  React.useEffect(() => {
    const s = t.startScreen || 'inbox';
    if (['inbox','groups','audit','settings'].includes(s)) {
      setScreen('inbox'); setTab(s);
    } else {
      // pre-seed payload for screens that need one
      if (s === 'detail' || s === 'signing' || s === 'result') {
        setPayload(ENVELOPES[0]);
      } else if (s === 'group') {
        setPayload(WALLETS[0]);
      }
      setScreen(s);
    }
  }, [t.startScreen]);

  const nav = (to, data) => {
    if (['inbox','groups','audit','settings'].includes(to)) {
      setTab(to);
      setScreen('inbox');
      return;
    }
    if (data !== undefined) setPayload(data);
    setScreen(to);
  };

  const tabBody = () => {
    if (tab === 'inbox')    return <InboxTab tk={tk} nav={nav} pending={ENVELOPES}/>;
    if (tab === 'groups')   return <GroupsTab tk={tk} nav={nav}/>;
    if (tab === 'audit')    return <AuditTab tk={tk} nav={nav}/>;
    if (tab === 'settings') return <SettingsTab tk={tk} nav={nav}/>;
    return null;
  };

  const body = () => {
    if (screen === 'onboarding')  return <OnboardingScreen tk={tk} nav={nav}/>;
    if (screen === 'initIdentity') return <InitIdentityStep tk={tk} nav={nav}/>;
    if (screen === 'initScan')     return <InitScanStep tk={tk} nav={nav}/>;
    if (screen === 'initConfirm')  return <InitConfirmStep tk={tk} nav={nav}/>;
    if (screen === 'initRegister') return <InitRegisterStep tk={tk} nav={nav}/>;
    if (screen === 'initDone')     return <InitDoneStep tk={tk} nav={nav}/>;
    if (screen === 'detail')      return <PendingDetailScreen tk={tk} nav={nav} envelope={payload || ENVELOPES[0]}/>;
    if (screen === 'signing')     return <SigningScreen tk={tk} nav={nav} vizVariant={t.vizVariant} envelope={payload || ENVELOPES[0]}/>;
    if (screen === 'result')      return <ResultScreen tk={tk} nav={nav} envelope={payload || ENVELOPES[0]}/>;
    if (screen === 'group')       return <GroupDetailScreen tk={tk} nav={nav} group={payload || WALLETS[0]}/>;
    if (screen === 'keygen')      return <KeygenWizard tk={tk} nav={nav} mode="keygen"/>;
    if (screen === 'reshare')     return <KeygenWizard tk={tk} nav={nav} mode="reshare"/>;
    if (screen === 'keygenDone')  return <KeygenDoneScreen tk={tk} nav={nav} mode={payload?.mode || 'keygen'}/>;
    if (screen === 'backup')      return <BackupScreen tk={tk} nav={nav}/>;
    // tabs view
    return (
      <>
        {tabBody()}
        <BottomTabs tk={tk} active={tab} onChange={setTab} badge={ENVELOPES.length}/>
      </>
    );
  };

  const screenFrame = (
    <div style={{
      width:'100%', height:'100%', background: tk.bgGrad, color:tk.text,
      position:'relative', overflow:'hidden',
      fontFamily:'-apple-system, "SF Pro Text", "PingFang SC", system-ui, sans-serif',
      WebkitFontSmoothing:'antialiased',
    }}>
      <div style={{
        position:'absolute', inset:0, pointerEvents:'none',
        background:`radial-gradient(60% 40% at 20% 0%, ${tk.accent}10, transparent 70%), radial-gradient(50% 30% at 100% 100%, ${tk.accent}08, transparent 70%)`,
      }}/>
      <div style={{ position:'absolute', top:0, left:0, right:0, zIndex:30 }}>
        <IOSStatusBar dark={true}/>
      </div>
      <div style={{
        position:'absolute', bottom:0, left:0, right:0, zIndex:60,
        height:34, display:'flex', justifyContent:'center', alignItems:'flex-end',
        paddingBottom:8, pointerEvents:'none',
      }}>
        <div style={{ width:139, height:5, borderRadius:100, background:'rgba(255,255,255,0.6)' }}/>
      </div>
      {body()}
    </div>
  );

  return (
    <div style={{
      minHeight:'100vh', width:'100%',
      display:'flex', alignItems:'center', justifyContent:'center',
      background:'#06070A',
      padding:'24px 0',
    }}>
      {t.showFrame ? (
        <div style={{
          width:402, height:874, borderRadius:48, overflow:'hidden', position:'relative',
          background:'#000',
          boxShadow:'0 40px 80px rgba(0,0,0,0.6), 0 0 0 1px rgba(255,255,255,0.06)',
        }}>
          <div style={{
            position:'absolute', top:11, left:'50%', transform:'translateX(-50%)',
            width:126, height:37, borderRadius:24, background:'#000', zIndex:50,
          }}/>
          {screenFrame}
        </div>
      ) : (
        <div style={{ width:402, height:874, borderRadius:24, overflow:'hidden', position:'relative' }}>
          {screenFrame}
        </div>
      )}

      <TweaksPanel title="Trine Signer">
        <TweakSection label="主题"/>
        <TweakRadio label="底色" value={t.theme} options={['midnight','onyx','graphite']}
          onChange={(v)=>setTweak('theme', v)}/>
        <TweakColor label="主色" value={t.accent}
          options={['#5EEAD4','#A78BFA','#60A5FA','#F7C25A','#FB7185']}
          onChange={(v)=>setTweak('accent', v)}/>

        <TweakSection label="签名动画"/>
        <TweakRadio label="样式" value={t.vizVariant} options={['orbit','pulse','stream']}
          onChange={(v)=>setTweak('vizVariant', v)}/>

        <TweakSection label="跳转"/>
        <TweakSelect label="起始页" value={t.startScreen}
          options={['onboarding','initIdentity','initScan','initConfirm','initRegister','initDone','inbox','groups','audit','settings','detail','signing','result','group','keygen','keygenDone','reshare','backup']}
          onChange={(v)=>setTweak('startScreen', v)}/>
        <TweakButton onClick={()=>{ setPayload(ENVELOPES[0]); setScreen('detail'); }}>
          ▶ 打开请求 #1
        </TweakButton>
        <TweakButton onClick={()=>{ setPayload(ENVELOPES[2]); setScreen('detail'); }}>
          ⚠ 打开可疑请求 (mismatch)
        </TweakButton>
        <TweakButton onClick={()=>{ setPayload(ENVELOPES[0]); setScreen('signing'); }}>
          预览 MPC 签名流程 →
        </TweakButton>
        <TweakButton onClick={()=>setScreen('onboarding')}>
          ▶ 重头跑初始化流程
        </TweakButton>

        <TweakSection label="陈列"/>
        <TweakToggle label="iPhone 边框" value={t.showFrame}
          onChange={(v)=>setTweak('showFrame', v)}/>
      </TweaksPanel>
    </div>
  );
}

ReactDOM.createRoot(document.getElementById('root')).render(<App/>);
