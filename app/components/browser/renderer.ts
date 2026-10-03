// Pixel-only document. It cannot navigate to or fetch the remote website.
// Rendering ACK is sent after image decode and a paint opportunity.
export const browserRendererHTML = `<!doctype html><html><head>
<meta name="viewport" content="width=device-width,initial-scale=1,maximum-scale=1">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src data:; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'none'; form-action 'none'; base-uri 'none'">
<style>html,body{margin:0;width:100%;height:100%;background:#171717;overflow:hidden}img{width:100%;height:100%;object-fit:contain;touch-action:none;user-select:none}</style></head>
<body><img id="screen" alt="Remote host browser"><script>
const screen=document.getElementById('screen');let width=1280,height=800,seq=0,ready=true,start=null,scroll=false,lastMove=0;
const post=v=>window.ReactNativeWebView.postMessage(JSON.stringify(v));
window.zenFrame=frame=>{
 if(!ready||frame.seq<=seq)return;
 ready=false;seq=frame.seq;
 width=frame.metadata.deviceWidth||1280;height=frame.metadata.deviceHeight||800;
 screen.onload=()=>requestAnimationFrame(()=>{ready=true;post({type:'ack',seq});});
 screen.onerror=()=>{ready=true;post({type:'ack',seq});};
 screen.src='data:image/jpeg;base64,'+frame.data;
};
window.zenScroll=value=>{scroll=value;};
const point=e=>{
 const r=screen.getBoundingClientRect(),scale=Math.min(r.width/width,r.height/height);
 return {x:Math.max(0,Math.min(width,(e.clientX-r.left-(r.width-width*scale)/2)/scale)),y:Math.max(0,Math.min(height,(e.clientY-r.top-(r.height-height*scale)/2)/scale))};
};
screen.onpointerdown=e=>{screen.setPointerCapture(e.pointerId);start=point(e);if(!scroll)post({type:'input',input:{kind:'mousePressed',...start,button:'left'}});};
screen.onpointermove=e=>{
 if(!start||Date.now()-lastMove<80)return;lastMove=Date.now();const p=point(e);
 if(scroll){post({type:'input',input:{kind:'mouseWheel',...p,dx:start.x-p.x,dy:start.y-p.y}});start=p;}
 else post({type:'input',input:{kind:'mouseMoved',...p,button:'left'}});
};
screen.onpointerup=e=>{if(start&&!scroll)post({type:'input',input:{kind:'mouseReleased',...point(e),button:'left'}});start=null;};
screen.onpointercancel=e=>{if(start&&!scroll)post({type:'input',input:{kind:'mouseReleased',...start,button:'left'}});start=null;};
screen.onwheel=e=>{e.preventDefault();post({type:'input',input:{kind:'mouseWheel',...point(e),dx:e.deltaX,dy:e.deltaY}});};
post({type:'ready'});
</script></body></html>`;
