// signing-viz.jsx — the hero TSS signing animation
// Three guardians (this device + cloud + hardware backup) exchanging shards
// over 3 rounds. Style variants: orbit | pulse | stream

function SigningViz({ accent = '#5EEAD4', variant = 'orbit', phase = 0, size = 320 }) {
  // phase 0..1, derived from outer animation
  const cx = size / 2, cy = size / 2 + 4;
  const r = size * 0.32;
  // 3 nodes at top, bottom-left, bottom-right
  const nodes = [
    { x: cx,           y: cy - r,                label: '本机 · iPhone',  sub: 'Party A' },
    { x: cx - r*0.866, y: cy + r*0.5,            label: 'Trine 协同服务', sub: 'Party B' },
    { x: cx + r*0.866, y: cy + r*0.5,            label: '云端守护',        sub: 'Party C' },
  ];

  // Round 1: commitments (0..0.33), Round 2: share exchange (0.33..0.66), Round 3: aggregate (0.66..1)
  const round = phase < 0.34 ? 1 : phase < 0.67 ? 2 : 3;

  return (
    <div style={{ position:'relative', width:size, height:size, margin:'0 auto' }}>
      <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} style={{position:'absolute', inset:0}}>
        <defs>
          <radialGradient id="trineGlow" cx="50%" cy="50%" r="50%">
            <stop offset="0%" stopColor={accent} stopOpacity="0.35"/>
            <stop offset="60%" stopColor={accent} stopOpacity="0.05"/>
            <stop offset="100%" stopColor={accent} stopOpacity="0"/>
          </radialGradient>
          <linearGradient id="trineLine" x1="0%" y1="0%" x2="100%" y2="0%">
            <stop offset="0%" stopColor={accent} stopOpacity="0.15"/>
            <stop offset="50%" stopColor={accent} stopOpacity="0.9"/>
            <stop offset="100%" stopColor={accent} stopOpacity="0.15"/>
          </linearGradient>
        </defs>

        {/* Soft glow under everything */}
        <circle cx={cx} cy={cy} r={size*0.42} fill="url(#trineGlow)" />

        {/* Inner geometric guides */}
        <circle cx={cx} cy={cy} r={r*0.55} stroke="rgba(255,255,255,0.06)" strokeWidth="1" strokeDasharray="2 4" fill="none" />
        <circle cx={cx} cy={cy} r={r} stroke="rgba(255,255,255,0.05)" strokeWidth="1" fill="none" />

        {/* Connection lines between nodes */}
        {[[0,1],[1,2],[2,0]].map(([i,j], k) => {
          const a = nodes[i], b = nodes[j];
          // each pair active in a different round (round 1: all faint; r2/r3 brighten)
          const active = phase > 0.2 + k*0.1;
          return (
            <line key={k}
              x1={a.x} y1={a.y} x2={b.x} y2={b.y}
              stroke={active ? accent : 'rgba(255,255,255,0.12)'}
              strokeWidth={active ? 1.2 : 0.7}
              strokeOpacity={active ? 0.55 : 1}
            />
          );
        })}

        {/* Animated shards travelling along edges (only in orbit/stream variants) */}
        {variant !== 'pulse' && [[0,1,0],[1,2,0.33],[2,0,0.66], [1,0,0.16],[2,1,0.5],[0,2,0.83]].map(([i,j,off], k) => {
          const a = nodes[i], b = nodes[j];
          const t = ((phase * 2.4 + off) % 1);
          const x = a.x + (b.x - a.x) * t;
          const y = a.y + (b.y - a.y) * t;
          const opacity = Math.sin(t * Math.PI);
          return <circle key={'s'+k} cx={x} cy={y} r={2.5} fill={accent} opacity={opacity} />;
        })}

        {/* Pulse rings emanating from each node (pulse variant emphasizes this) */}
        {nodes.map((n, i) => {
          const t = ((phase * 1.6 + i * 0.33) % 1);
          const pr = 14 + t * 30;
          return <circle key={'p'+i} cx={n.x} cy={n.y} r={pr} stroke={accent} strokeWidth="1" fill="none" opacity={(1-t) * (variant==='pulse'?0.7:0.4)} />;
        })}

        {/* Nodes */}
        {nodes.map((n, i) => {
          const ringT = Math.max(0, Math.min(1, (phase - i*0.08) * 1.4));
          const C = 2 * Math.PI * 18;
          return (
            <g key={i} transform={`translate(${n.x},${n.y})`}>
              {/* progress ring */}
              <circle r="18" stroke="rgba(255,255,255,0.1)" strokeWidth="2" fill="none" />
              <circle r="18" stroke={accent} strokeWidth="2" fill="none"
                strokeDasharray={`${C * ringT} ${C}`} strokeDashoffset={C * 0.25}
                transform="rotate(-90)" strokeLinecap="round" />
              {/* core */}
              <circle r="11" fill="#0a0d14" />
              <circle r="11" stroke={accent} strokeWidth="1" fill="none" opacity="0.6" />
              <text textAnchor="middle" dy="0.34em" fontSize="11" fontWeight="600" fill={accent} fontFamily="system-ui">{['A','B','C'][i]}</text>
            </g>
          );
        })}

        {/* Center: triangle that fills with rounds */}
        <g opacity="0.9">
          <polygon
            points={nodes.map(n=>`${cx + (n.x-cx)*0.32},${cy + (n.y-cy)*0.32}`).join(' ')}
            stroke={accent} strokeWidth="1" fill={accent} fillOpacity={0.04 + phase*0.18}
          />
        </g>

        {/* Center signature icon */}
        <g transform={`translate(${cx},${cy})`}>
          <text textAnchor="middle" dy="0.34em" fontSize="22" fontWeight="700" fill="#fff" fontFamily="system-ui" letterSpacing="-0.5">
            {round === 3 ? '✓' : '⌬'}
          </text>
        </g>
      </svg>

      {/* Node labels (HTML so it scales nicely) */}
      {nodes.map((n,i)=> {
        const dx = i===0 ? 0 : (i===1 ? -1 : 1);
        const dy = i===0 ? -1 : 1;
        return (
          <div key={i} style={{
            position:'absolute',
            left: n.x + dx*0 - 50,
            top: n.y + dy*22,
            width: 100,
            textAlign: 'center',
            fontSize: 10.5,
            color: 'rgba(255,255,255,0.55)',
            fontFamily:'system-ui',
            letterSpacing: 0.2,
            pointerEvents: 'none',
          }}>
            <div style={{color:'rgba(255,255,255,0.85)', fontWeight:500}}>{n.label}</div>
            <div style={{opacity:.7, fontSize:9.5, marginTop:1, fontVariant:'all-small-caps'}}>{n.sub}</div>
          </div>
        );
      })}
    </div>
  );
}

// Drives a continuous phase 0..1 if no phase is supplied
function useSigningPhase(running, duration = 5200) {
  const [t, setT] = React.useState(0);
  React.useEffect(() => {
    if (!running) return;
    let raf, start = performance.now();
    const tick = (now) => {
      const elapsed = (now - start) / duration;
      const phase = Math.min(1, elapsed);
      setT(phase);
      if (phase < 1) raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(raf);
  }, [running, duration]);
  return [t, () => setT(0)];
}

Object.assign(window, { SigningViz, useSigningPhase });
