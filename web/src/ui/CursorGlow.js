// Port of FIZMA.zip/components/ui/cursor-glow.tsx. Kept inside the access screen
// so mouse look and the ship's pointer lock never receive an extra cursor layer.
export const FIZMA_CURSOR = Object.freeze({idle:400,hover:500,core:32,coreOpacity:.15,breakpoint:1024});

export function installCursorGlow(root = document.getElementById('auth-screen')) {
  if (!root) return {dispose(){}};
  const glow = document.createElement('div');
  const core = document.createElement('div');
  glow.className = 'fizma-cursor-glow'; core.className = 'fizma-cursor-core';
  for (const node of [glow,core]) { node.setAttribute('aria-hidden','true'); root.append(node); }
  let frame = 0, x = 0, y = 0;
  const hide = () => {
    if (frame) cancelAnimationFrame(frame);
    frame = 0; glow.style.opacity = '0'; core.style.opacity = '0';
  };
  const move = event => {
    if (root.hidden || document.pointerLockElement || event.pointerType === 'touch') { hide(); return; }
    x = event.clientX; y = event.clientY;
    if (frame) return;
    frame = requestAnimationFrame(() => {
      frame = 0;
      if (root.hidden || document.pointerLockElement) { hide(); return; }
      for (const node of [glow,core]) { node.style.left = x+'px'; node.style.top = y+'px'; }
      glow.style.opacity = '1'; core.style.opacity = String(FIZMA_CURSOR.coreOpacity);
    });
  };
  const hover = event => {
    const interactive = !!event.target?.closest?.('a, button, [role="button"], input, textarea, select');
    glow.style.width = glow.style.height = (interactive ? FIZMA_CURSOR.hover : FIZMA_CURSOR.idle)+'px';
  };
  window.addEventListener('mousemove',move,{passive:true});
  document.body.addEventListener('mouseleave',hide);
  document.addEventListener('mouseover',hover,{passive:true});
  document.addEventListener('pointerlockchange',hide);
  const observer = typeof MutationObserver === 'function' ? new MutationObserver(hide) : null;
  observer?.observe(root,{attributes:true,attributeFilter:['hidden']});
  const dispose = () => {
    hide(); observer?.disconnect();
    window.removeEventListener('mousemove',move);
    document.body.removeEventListener('mouseleave',hide);
    document.removeEventListener('mouseover',hover);
    document.removeEventListener('pointerlockchange',hide);
    window.removeEventListener('pagehide',dispose);
    glow.remove(); core.remove();
  };
  window.addEventListener('pagehide',dispose,{once:true});
  return {dispose};
}
