import{b4 as I,b3 as dr,bx as pe,aY as we,aX as Ke,Y as K,a4 as Ae,as as ae,aW as be,aU as se,bp as tt,a6 as Q,F as Be,C as Zt,aa as U,b2 as me,aj as no,bA as ot,a as oo,aq as p,T as ao,bk as G,bs as jr,aS as pt,aD as Nr,a3 as io,ap as Wt,bw as cr,aI as lo,aE as rt,ao as xt,bb as at,a$ as so,aG as co,ax as Jt,p as uo,aw as Xe,t as Ur,bl as qe,M as Ht,b as fo,l as ur,ae as ho,U as fr,az as hr,S as vt,aJ as po,aF as pr,G as vo,bj as Qt,aC as bo,aA as go,av as mo,aB as Vr,ai as yo,s as xo,ar as wo,r as Co,q as $o,u as M,v as y,z as Te,x as z,y as $,w as So,n as _o,bn as Ce,bt as re,by as er,bv as Xr,ah as Pt,bu as Oe,b1 as Po,aH as vr,aQ as tr,b8 as ye,X as zo,K as Gr,m as To,bq as Kr,D as fe,ad as qr,O as ko,H,N as rr,br as it,a5 as W,al as Ne,L as br,P as Io,R as _e,af as Eo,f as Ao,b7 as Dt,c as nr,E as Bo,W as Yr,I as Mo,k as Oo,aR as Lo,bh as or,Q as Ro,ay as Fo,ak as Wo,ac as jt,at as Ho,au as Do,aL as jo,B as xe,bo as gr,aK as No,aV as Zr,b0 as Uo,b5 as Vo,V as zt,bf as Xo,o as Go,bg as Ko,am as qo,ag as Yo,a_ as Y,a2 as Ee,$ as te,bi as ve,aT as Tt,a1 as ke,aP as Zo,_ as Ye,a0 as le,bm as A,d as wt,bz as E,a8 as F,b6 as Jr,i as nt,aN as Jo,A as Ie,a7 as Qo,ba as ea,h as bt,g as Qr,J as ta,aM as ra,b9 as na,bd as oa,ab as aa,aZ as ia,an as la,j as sa,a9 as da,e as ca,Z as ua}from"./tokens-DIEMJioJ.js";let gt=[];const en=new WeakMap;function fa(){gt.forEach(e=>e(...en.get(e))),gt=[]}function ha(e,...t){en.set(e,t),!gt.includes(e)&&gt.push(e)===1&&requestAnimationFrame(fa)}function pa(e){const t=I(!!e.value);if(t.value)return dr(t);const r=pe(e,n=>{n&&(t.value=!0,r())});return dr(t)}const va=typeof window<"u";let Ve,et;const ba=()=>{var e,t;Ve=va?(t=(e=document)===null||e===void 0?void 0:e.fonts)===null||t===void 0?void 0:t.ready:void 0,et=!1,Ve!==void 0?Ve.then(()=>{et=!0}):et=!0};ba();function tn(e){if(et)return;let t=!1;we(()=>{et||Ve==null||Ve.then(()=>{t||e()})}),Ke(()=>{t=!0})}function Nt(e,t){return K(()=>{for(const r of t)if(e[r]!==void 0)return e[r];return e[t[t.length-1]]})}const ga=Ae("n-internal-select-menu-body"),rn=Ae("n-drawer-body"),nn=Ae("n-modal-body"),on=Ae("n-popover-body"),an="__disabled__";function Ge(e){const t=ae(nn,null),r=ae(rn,null),n=ae(on,null),o=ae(ga,null),l=I();if(typeof document<"u"){l.value=document.fullscreenElement;const s=()=>{l.value=document.fullscreenElement};we(()=>{be("fullscreenchange",document,s)}),Ke(()=>{se("fullscreenchange",document,s)})}return tt(()=>{var s;const{to:i}=e;return i!==void 0?i===!1?an:i===!0?l.value||"body":i:t!=null&&t.value?(s=t.value.$el)!==null&&s!==void 0?s:t.value:r!=null&&r.value?r.value:n!=null&&n.value?n.value:o!=null&&o.value?o.value:i??(l.value||"body")})}Ge.tdkey=an;Ge.propTo={type:[String,Object,Boolean],default:void 0};function Ut(e,t,r="default"){const n=t[r];if(n===void 0)throw new Error(`[vueuc/${e}]: slot[${r}] is empty.`);return n()}function Vt(e,t=!0,r=[]){return e.forEach(n=>{if(n!==null){if(typeof n!="object"){(typeof n=="string"||typeof n=="number")&&r.push(Q(String(n)));return}if(Array.isArray(n)){Vt(n,t,r);return}if(n.type===Be){if(n.children===null)return;Array.isArray(n.children)&&Vt(n.children,t,r)}else n.type!==Zt&&r.push(n)}}),r}function mr(e,t,r="default"){const n=t[r];if(n===void 0)throw new Error(`[vueuc/${e}]: slot[${r}] is empty.`);const o=Vt(n());if(o.length===1)return o[0];throw new Error(`[vueuc/${e}]: slot[${r}] should have exactly one child.`)}let Pe=null;function ln(){if(Pe===null&&(Pe=document.getElementById("v-binder-view-measurer"),Pe===null)){Pe=document.createElement("div"),Pe.id="v-binder-view-measurer";const{style:e}=Pe;e.position="fixed",e.left="0",e.right="0",e.top="0",e.bottom="0",e.pointerEvents="none",e.visibility="hidden",document.body.appendChild(Pe)}return Pe.getBoundingClientRect()}function ma(e,t){const r=ln();return{top:t,left:e,height:0,width:0,right:r.width-e,bottom:r.height-t}}function kt(e){const t=e.getBoundingClientRect(),r=ln();return{left:t.left-r.left,top:t.top-r.top,bottom:r.height+r.top-t.bottom,right:r.width+r.left-t.right,width:t.width,height:t.height}}function ya(e){return e.nodeType===9?null:e.parentNode}function sn(e){if(e===null)return null;const t=ya(e);if(t===null)return null;if(t.nodeType===9)return document;if(t.nodeType===1){const{overflow:r,overflowX:n,overflowY:o}=getComputedStyle(t);if(/(auto|scroll|overlay)/.test(r+o+n))return t}return sn(t)}const xa=U({name:"Binder",props:{syncTargetWithParent:Boolean,syncTarget:{type:Boolean,default:!0}},setup(e){var t;me("VBinder",(t=no())===null||t===void 0?void 0:t.proxy);const r=ae("VBinder",null),n=I(null),o=v=>{n.value=v,r&&e.syncTargetWithParent&&r.setTargetRef(v)};let l=[];const s=()=>{let v=n.value;for(;v=sn(v),v!==null;)l.push(v);for(const C of l)be("scroll",C,g,!0)},i=()=>{for(const v of l)se("scroll",v,g,!0);l=[]},a=new Set,u=v=>{a.size===0&&s(),a.has(v)||a.add(v)},d=v=>{a.has(v)&&a.delete(v),a.size===0&&i()},g=()=>{ha(f)},f=()=>{a.forEach(v=>v())},c=new Set,h=v=>{c.size===0&&be("resize",window,m),c.has(v)||c.add(v)},b=v=>{c.has(v)&&c.delete(v),c.size===0&&se("resize",window,m)},m=()=>{c.forEach(v=>v())};return Ke(()=>{se("resize",window,m),i()}),{targetRef:n,setTargetRef:o,addScrollListener:u,removeScrollListener:d,addResizeListener:h,removeResizeListener:b}},render(){return Ut("binder",this.$slots)}}),wa=U({name:"Target",setup(){const{setTargetRef:e,syncTarget:t}=ae("VBinder");return{syncTarget:t,setTargetDirective:{mounted:e,updated:e}}},render(){const{syncTarget:e,setTargetDirective:t}=this;return e?ot(mr("follower",this.$slots),[[t]]):mr("follower",this.$slots)}}),He="@@mmoContext",Ca={mounted(e,{value:t}){e[He]={handler:void 0},typeof t=="function"&&(e[He].handler=t,be("mousemoveoutside",e,t))},updated(e,{value:t}){const r=e[He];typeof t=="function"?r.handler?r.handler!==t&&(se("mousemoveoutside",e,r.handler),r.handler=t,be("mousemoveoutside",e,t)):(e[He].handler=t,be("mousemoveoutside",e,t)):r.handler&&(se("mousemoveoutside",e,r.handler),r.handler=void 0)},unmounted(e){const{handler:t}=e[He];t&&se("mousemoveoutside",e,t),e[He].handler=void 0}},De="@@coContext",yr={mounted(e,{value:t,modifiers:r}){e[De]={handler:void 0},typeof t=="function"&&(e[De].handler=t,be("clickoutside",e,t,{capture:r.capture}))},updated(e,{value:t,modifiers:r}){const n=e[De];typeof t=="function"?n.handler?n.handler!==t&&(se("clickoutside",e,n.handler,{capture:r.capture}),n.handler=t,be("clickoutside",e,t,{capture:r.capture})):(e[De].handler=t,be("clickoutside",e,t,{capture:r.capture})):n.handler&&(se("clickoutside",e,n.handler,{capture:r.capture}),n.handler=void 0)},unmounted(e,{modifiers:t}){const{handler:r}=e[De];r&&se("clickoutside",e,r,{capture:t.capture}),e[De].handler=void 0}};function $a(e,t){console.error(`[vdirs/${e}]: ${t}`)}class Sa{constructor(){this.elementZIndex=new Map,this.nextZIndex=2e3}get elementCount(){return this.elementZIndex.size}ensureZIndex(t,r){const{elementZIndex:n}=this;if(r!==void 0){t.style.zIndex=`${r}`,n.delete(t);return}const{nextZIndex:o}=this;n.has(t)&&n.get(t)+1===this.nextZIndex||(t.style.zIndex=`${o}`,n.set(t,o),this.nextZIndex=o+1,this.squashState())}unregister(t,r){const{elementZIndex:n}=this;n.has(t)?n.delete(t):r===void 0&&$a("z-index-manager/unregister-element","Element not found when unregistering."),this.squashState()}squashState(){const{elementCount:t}=this;t||(this.nextZIndex=2e3),this.nextZIndex-t>2500&&this.rearrange()}rearrange(){const t=Array.from(this.elementZIndex.entries());t.sort((r,n)=>r[1]-n[1]),this.nextZIndex=2e3,t.forEach(r=>{const n=r[0],o=this.nextZIndex++;`${o}`!==n.style.zIndex&&(n.style.zIndex=`${o}`)})}}const It=new Sa,je="@@ziContext",dn={mounted(e,t){const{value:r={}}=t,{zIndex:n,enabled:o}=r;e[je]={enabled:!!o,initialized:!1},o&&(It.ensureZIndex(e,n),e[je].initialized=!0)},updated(e,t){const{value:r={}}=t,{zIndex:n,enabled:o}=r,l=e[je].enabled;o&&!l&&(It.ensureZIndex(e,n),e[je].initialized=!0),e[je].enabled=!!o},unmounted(e,t){if(!e[je].initialized)return;const{value:r={}}=t,{zIndex:n}=r;It.unregister(e,n)}},{c:Ue}=oo(),cn="vueuc-style";function xr(e){return typeof e=="string"?document.querySelector(e):e()||null}const _a=U({name:"LazyTeleport",props:{to:{type:[String,Object],default:void 0},disabled:Boolean,show:{type:Boolean,required:!0}},setup(e){return{showTeleport:pa(G(e,"show")),mergedTo:K(()=>{const{to:t}=e;return t??"body"})}},render(){return this.showTeleport?this.disabled?Ut("lazy-teleport",this.$slots):p(ao,{disabled:this.disabled,to:this.mergedTo},Ut("lazy-teleport",this.$slots)):null}}),ut={top:"bottom",bottom:"top",left:"right",right:"left"},wr={start:"end",center:"center",end:"start"},Et={top:"height",bottom:"height",left:"width",right:"width"},Pa={"bottom-start":"top left",bottom:"top center","bottom-end":"top right","top-start":"bottom left",top:"bottom center","top-end":"bottom right","right-start":"top left",right:"center left","right-end":"bottom left","left-start":"top right",left:"center right","left-end":"bottom right"},za={"bottom-start":"bottom left",bottom:"bottom center","bottom-end":"bottom right","top-start":"top left",top:"top center","top-end":"top right","right-start":"top right",right:"center right","right-end":"bottom right","left-start":"top left",left:"center left","left-end":"bottom left"},Ta={"bottom-start":"right","bottom-end":"left","top-start":"right","top-end":"left","right-start":"bottom","right-end":"top","left-start":"bottom","left-end":"top"},Cr={top:!0,bottom:!1,left:!0,right:!1},$r={top:"end",bottom:"start",left:"end",right:"start"};function ka(e,t,r,n,o,l){if(!o||l)return{placement:e,top:0,left:0};const[s,i]=e.split("-");let a=i??"center",u={top:0,left:0};const d=(c,h,b)=>{let m=0,v=0;const C=r[c]-t[h]-t[c];return C>0&&n&&(b?v=Cr[h]?C:-C:m=Cr[h]?C:-C),{left:m,top:v}},g=s==="left"||s==="right";if(a!=="center"){const c=Ta[e],h=ut[c],b=Et[c];if(r[b]>t[b]){if(t[c]+t[b]<r[b]){const m=(r[b]-t[b])/2;t[c]<m||t[h]<m?t[c]<t[h]?(a=wr[i],u=d(b,h,g)):u=d(b,c,g):a="center"}}else r[b]<t[b]&&t[h]<0&&t[c]>t[h]&&(a=wr[i])}else{const c=s==="bottom"||s==="top"?"left":"top",h=ut[c],b=Et[c],m=(r[b]-t[b])/2;(t[c]<m||t[h]<m)&&(t[c]>t[h]?(a=$r[c],u=d(b,c,g)):(a=$r[h],u=d(b,h,g)))}let f=s;return t[s]<r[Et[s]]&&t[s]<t[ut[s]]&&(f=ut[s]),{placement:a!=="center"?`${f}-${a}`:f,left:u.left,top:u.top}}function Ia(e,t){return t?za[e]:Pa[e]}function Ea(e,t,r,n,o,l){if(l)switch(e){case"bottom-start":return{top:`${Math.round(r.top-t.top+r.height)}px`,left:`${Math.round(r.left-t.left)}px`,transform:"translateY(-100%)"};case"bottom-end":return{top:`${Math.round(r.top-t.top+r.height)}px`,left:`${Math.round(r.left-t.left+r.width)}px`,transform:"translateX(-100%) translateY(-100%)"};case"top-start":return{top:`${Math.round(r.top-t.top)}px`,left:`${Math.round(r.left-t.left)}px`,transform:""};case"top-end":return{top:`${Math.round(r.top-t.top)}px`,left:`${Math.round(r.left-t.left+r.width)}px`,transform:"translateX(-100%)"};case"right-start":return{top:`${Math.round(r.top-t.top)}px`,left:`${Math.round(r.left-t.left+r.width)}px`,transform:"translateX(-100%)"};case"right-end":return{top:`${Math.round(r.top-t.top+r.height)}px`,left:`${Math.round(r.left-t.left+r.width)}px`,transform:"translateX(-100%) translateY(-100%)"};case"left-start":return{top:`${Math.round(r.top-t.top)}px`,left:`${Math.round(r.left-t.left)}px`,transform:""};case"left-end":return{top:`${Math.round(r.top-t.top+r.height)}px`,left:`${Math.round(r.left-t.left)}px`,transform:"translateY(-100%)"};case"top":return{top:`${Math.round(r.top-t.top)}px`,left:`${Math.round(r.left-t.left+r.width/2)}px`,transform:"translateX(-50%)"};case"right":return{top:`${Math.round(r.top-t.top+r.height/2)}px`,left:`${Math.round(r.left-t.left+r.width)}px`,transform:"translateX(-100%) translateY(-50%)"};case"left":return{top:`${Math.round(r.top-t.top+r.height/2)}px`,left:`${Math.round(r.left-t.left)}px`,transform:"translateY(-50%)"};case"bottom":default:return{top:`${Math.round(r.top-t.top+r.height)}px`,left:`${Math.round(r.left-t.left+r.width/2)}px`,transform:"translateX(-50%) translateY(-100%)"}}switch(e){case"bottom-start":return{top:`${Math.round(r.top-t.top+r.height+n)}px`,left:`${Math.round(r.left-t.left+o)}px`,transform:""};case"bottom-end":return{top:`${Math.round(r.top-t.top+r.height+n)}px`,left:`${Math.round(r.left-t.left+r.width+o)}px`,transform:"translateX(-100%)"};case"top-start":return{top:`${Math.round(r.top-t.top+n)}px`,left:`${Math.round(r.left-t.left+o)}px`,transform:"translateY(-100%)"};case"top-end":return{top:`${Math.round(r.top-t.top+n)}px`,left:`${Math.round(r.left-t.left+r.width+o)}px`,transform:"translateX(-100%) translateY(-100%)"};case"right-start":return{top:`${Math.round(r.top-t.top+n)}px`,left:`${Math.round(r.left-t.left+r.width+o)}px`,transform:""};case"right-end":return{top:`${Math.round(r.top-t.top+r.height+n)}px`,left:`${Math.round(r.left-t.left+r.width+o)}px`,transform:"translateY(-100%)"};case"left-start":return{top:`${Math.round(r.top-t.top+n)}px`,left:`${Math.round(r.left-t.left+o)}px`,transform:"translateX(-100%)"};case"left-end":return{top:`${Math.round(r.top-t.top+r.height+n)}px`,left:`${Math.round(r.left-t.left+o)}px`,transform:"translateX(-100%) translateY(-100%)"};case"top":return{top:`${Math.round(r.top-t.top+n)}px`,left:`${Math.round(r.left-t.left+r.width/2+o)}px`,transform:"translateY(-100%) translateX(-50%)"};case"right":return{top:`${Math.round(r.top-t.top+r.height/2+n)}px`,left:`${Math.round(r.left-t.left+r.width+o)}px`,transform:"translateY(-50%)"};case"left":return{top:`${Math.round(r.top-t.top+r.height/2+n)}px`,left:`${Math.round(r.left-t.left+o)}px`,transform:"translateY(-50%) translateX(-100%)"};case"bottom":default:return{top:`${Math.round(r.top-t.top+r.height+n)}px`,left:`${Math.round(r.left-t.left+r.width/2+o)}px`,transform:"translateX(-50%)"}}}const Aa=Ue([Ue(".v-binder-follower-container",{position:"absolute",left:"0",right:"0",top:"0",height:"0",pointerEvents:"none",zIndex:"auto"}),Ue(".v-binder-follower-content",{position:"absolute",zIndex:"auto"},[Ue("> *",{pointerEvents:"all"})])]),Ba=U({name:"Follower",inheritAttrs:!1,props:{show:Boolean,enabled:{type:Boolean,default:void 0},placement:{type:String,default:"bottom"},syncTrigger:{type:Array,default:["resize","scroll"]},to:[String,Object],flip:{type:Boolean,default:!0},internalShift:Boolean,x:Number,y:Number,width:String,minWidth:String,containerClass:String,teleportDisabled:Boolean,zindexable:{type:Boolean,default:!0},zIndex:Number,overlap:Boolean},setup(e){const t=ae("VBinder"),r=tt(()=>e.enabled!==void 0?e.enabled:e.show),n=I(null),o=I(null),l=()=>{const{syncTrigger:f}=e;f.includes("scroll")&&t.addScrollListener(a),f.includes("resize")&&t.addResizeListener(a)},s=()=>{t.removeScrollListener(a),t.removeResizeListener(a)};we(()=>{r.value&&(a(),l())});const i=jr();Aa.mount({id:"vueuc/binder",head:!0,anchorMetaName:cn,ssr:i}),Ke(()=>{s()}),tn(()=>{r.value&&a()});const a=()=>{if(!r.value)return;const f=n.value;if(f===null)return;const c=t.targetRef,{x:h,y:b,overlap:m}=e,v=h!==void 0&&b!==void 0?ma(h,b):kt(c);f.style.setProperty("--v-target-width",`${Math.round(v.width)}px`),f.style.setProperty("--v-target-height",`${Math.round(v.height)}px`);const{width:C,minWidth:R,placement:B,internalShift:T,flip:P}=e;f.setAttribute("v-placement",B),m?f.setAttribute("v-overlap",""):f.removeAttribute("v-overlap");const{style:S}=f;C==="target"?S.width=`${v.width}px`:C!==void 0?S.width=C:S.width="",R==="target"?S.minWidth=`${v.width}px`:R!==void 0?S.minWidth=R:S.minWidth="";const D=kt(f),j=kt(o.value),{left:O,top:Z,placement:N}=ka(B,v,D,T,P,m),X=Ia(N,m),{left:ne,top:_,transform:V}=Ea(N,j,v,Z,O,m);f.setAttribute("v-placement",N),f.style.setProperty("--v-offset-left",`${Math.round(O)}px`),f.style.setProperty("--v-offset-top",`${Math.round(Z)}px`),f.style.transform=`translateX(${ne}) translateY(${_}) ${V}`,f.style.setProperty("--v-transform-origin",X),f.style.transformOrigin=X};pe(r,f=>{f?(l(),u()):s()});const u=()=>{pt().then(a).catch(f=>console.error(f))};["placement","x","y","internalShift","flip","width","overlap","minWidth"].forEach(f=>{pe(G(e,f),a)}),["teleportDisabled"].forEach(f=>{pe(G(e,f),u)}),pe(G(e,"syncTrigger"),f=>{f.includes("resize")?t.addResizeListener(a):t.removeResizeListener(a),f.includes("scroll")?t.addScrollListener(a):t.removeScrollListener(a)});const d=Nr(),g=tt(()=>{const{to:f}=e;if(f!==void 0)return f;d.value});return{VBinder:t,mergedEnabled:r,offsetContainerRef:o,followerRef:n,mergedTo:g,syncPosition:a}},render(){return p(_a,{show:this.show,to:this.mergedTo,disabled:this.teleportDisabled},{default:()=>{var e,t;const r=p("div",{class:["v-binder-follower-container",this.containerClass],ref:"offsetContainerRef"},[p("div",{class:"v-binder-follower-content",ref:"followerRef"},(t=(e=this.$slots).default)===null||t===void 0?void 0:t.call(e))]);return this.zindexable?ot(r,[[dn,{enabled:this.mergedEnabled,zIndex:this.zIndex}]]):r}})}}),Ma=Ue(".v-x-scroll",{overflow:"auto",scrollbarWidth:"none"},[Ue("&::-webkit-scrollbar",{width:0,height:0})]),Oa=U({name:"XScroll",props:{disabled:Boolean,onScroll:Function},setup(){const e=I(null);function t(o){!(o.currentTarget.offsetWidth<o.currentTarget.scrollWidth)||o.deltaY===0||(o.currentTarget.scrollLeft+=o.deltaY+o.deltaX,o.preventDefault())}const r=jr();return Ma.mount({id:"vueuc/x-scroll",head:!0,anchorMetaName:cn,ssr:r}),Object.assign({selfRef:e,handleWheel:t},{scrollTo(...o){var l;(l=e.value)===null||l===void 0||l.scrollTo(...o)}})},render(){return p("div",{ref:"selfRef",onScroll:this.onScroll,onWheel:this.disabled?void 0:this.handleWheel,class:"v-x-scroll"},this.$slots)}});function un(e){return e instanceof HTMLElement}function fn(e){for(let t=0;t<e.childNodes.length;t++){const r=e.childNodes[t];if(un(r)&&(pn(r)||fn(r)))return!0}return!1}function hn(e){for(let t=e.childNodes.length-1;t>=0;t--){const r=e.childNodes[t];if(un(r)&&(pn(r)||hn(r)))return!0}return!1}function pn(e){if(!La(e))return!1;try{e.focus({preventScroll:!0})}catch{}return document.activeElement===e}function La(e){if(e.tabIndex>0||e.tabIndex===0&&e.getAttribute("tabIndex")!==null)return!0;if(e.getAttribute("disabled"))return!1;switch(e.nodeName){case"A":return!!e.href&&e.rel!=="ignore";case"INPUT":return e.type!=="hidden"&&e.type!=="file";case"SELECT":case"TEXTAREA":return!0;default:return!1}}let Qe=[];const Ra=U({name:"FocusTrap",props:{disabled:Boolean,active:Boolean,autoFocus:{type:Boolean,default:!0},onEsc:Function,initialFocusTo:[String,Function],finalFocusTo:[String,Function],returnFocusOnDeactivated:{type:Boolean,default:!0}},setup(e){const t=io(),r=I(null),n=I(null);let o=!1,l=!1;const s=typeof document>"u"?null:document.activeElement;function i(){return Qe[Qe.length-1]===t}function a(m){var v;m.code==="Escape"&&i()&&((v=e.onEsc)===null||v===void 0||v.call(e,m))}we(()=>{pe(()=>e.active,m=>{m?(g(),be("keydown",document,a)):(se("keydown",document,a),o&&f())},{immediate:!0})}),Ke(()=>{se("keydown",document,a),o&&f()});function u(m){if(!l&&i()){const v=d();if(v===null||v.contains(Wt(m)))return;c("first")}}function d(){const m=r.value;if(m===null)return null;let v=m;for(;v=v.nextSibling,!(v===null||v instanceof Element&&v.tagName==="DIV"););return v}function g(){var m;if(!e.disabled){if(Qe.push(t),e.autoFocus){const{initialFocusTo:v}=e;v===void 0?c("first"):(m=xr(v))===null||m===void 0||m.focus({preventScroll:!0})}o=!0,document.addEventListener("focus",u,!0)}}function f(){var m;if(e.disabled||(document.removeEventListener("focus",u,!0),Qe=Qe.filter(C=>C!==t),i()))return;const{finalFocusTo:v}=e;v!==void 0?(m=xr(v))===null||m===void 0||m.focus({preventScroll:!0}):e.returnFocusOnDeactivated&&s instanceof HTMLElement&&(l=!0,s.focus({preventScroll:!0}),l=!1)}function c(m){if(i()&&e.active){const v=r.value,C=n.value;if(v!==null&&C!==null){const R=d();if(R==null||R===C){l=!0,v.focus({preventScroll:!0}),l=!1;return}l=!0;const B=m==="first"?fn(R):hn(R);l=!1,B||(l=!0,v.focus({preventScroll:!0}),l=!1)}}}function h(m){if(l)return;const v=d();v!==null&&(m.relatedTarget!==null&&v.contains(m.relatedTarget)?c("last"):c("first"))}function b(m){l||(m.relatedTarget!==null&&m.relatedTarget===r.value?c("last"):c("first"))}return{focusableStartRef:r,focusableEndRef:n,focusableStyle:"position: absolute; height: 0; width: 0;",handleStartFocus:h,handleEndFocus:b}},render(){const{default:e}=this.$slots;if(e===void 0)return null;if(this.disabled)return e();const{active:t,focusableStyle:r}=this;return p(Be,null,[p("div",{"aria-hidden":"true",tabindex:t?"0":"-1",ref:"focusableStartRef",style:r,onFocus:this.handleStartFocus}),e(),p("div",{"aria-hidden":"true",style:r,ref:"focusableEndRef",tabindex:t?"0":"-1",onFocus:this.handleEndFocus})])}});let At;function Fa(){return At===void 0&&(At=navigator.userAgent.includes("Node.js")||navigator.userAgent.includes("jsdom")),At}function Me(e,t=!0,r=[]){return e.forEach(n=>{if(n!==null){if(typeof n!="object"){(typeof n=="string"||typeof n=="number")&&r.push(Q(String(n)));return}if(Array.isArray(n)){Me(n,t,r);return}if(n.type===Be){if(n.children===null)return;Array.isArray(n.children)&&Me(n.children,t,r)}else{if(n.type===Zt&&t)return;r.push(n)}}}),r}function Sr(e,t="default",r=void 0){const n=e[t];if(!n)return cr("getFirstSlotVNode",`slot[${t}] is empty`),null;const o=Me(n(r));return o.length===1?o[0]:(cr("getFirstSlotVNode",`slot[${t}] should have exactly one child`),null)}function Wa(e,t="default",r=[]){const o=e.$slots[t];return o===void 0?r:o()}function vn(e,t=[],r){const n={};return t.forEach(o=>{n[o]=e[o]}),Object.assign(n,r)}var Ha=/\s/;function Da(e){for(var t=e.length;t--&&Ha.test(e.charAt(t)););return t}var ja=/^\s+/;function Na(e){return e&&e.slice(0,Da(e)+1).replace(ja,"")}var _r=NaN,Ua=/^[-+]0x[0-9a-f]+$/i,Va=/^0b[01]+$/i,Xa=/^0o[0-7]+$/i,Ga=parseInt;function Pr(e){if(typeof e=="number")return e;if(lo(e))return _r;if(rt(e)){var t=typeof e.valueOf=="function"?e.valueOf():e;e=rt(t)?t+"":t}if(typeof e!="string")return e===0?e:+e;e=Na(e);var r=Va.test(e);return r||Xa.test(e)?Ga(e.slice(2),r?2:8):Ua.test(e)?_r:+e}var Xt=xt(at,"WeakMap"),Ka=so(Object.keys,Object),qa=Object.prototype,Ya=qa.hasOwnProperty;function Za(e){if(!co(e))return Ka(e);var t=[];for(var r in Object(e))Ya.call(e,r)&&r!="constructor"&&t.push(r);return t}function ar(e){return Jt(e)?uo(e):Za(e)}function Ja(e,t){for(var r=-1,n=t.length,o=e.length;++r<n;)e[o+r]=t[r];return e}function Qa(e,t){for(var r=-1,n=e==null?0:e.length,o=0,l=[];++r<n;){var s=e[r];t(s,r,e)&&(l[o++]=s)}return l}function ei(){return[]}var ti=Object.prototype,ri=ti.propertyIsEnumerable,zr=Object.getOwnPropertySymbols,ni=zr?function(e){return e==null?[]:(e=Object(e),Qa(zr(e),function(t){return ri.call(e,t)}))}:ei;function oi(e,t,r){var n=t(e);return Xe(e)?n:Ja(n,r(e))}function Tr(e){return oi(e,ar,ni)}var Gt=xt(at,"DataView"),Kt=xt(at,"Promise"),qt=xt(at,"Set"),kr="[object Map]",ai="[object Object]",Ir="[object Promise]",Er="[object Set]",Ar="[object WeakMap]",Br="[object DataView]",ii=qe(Gt),li=qe(Ht),si=qe(Kt),di=qe(qt),ci=qe(Xt),ze=Ur;(Gt&&ze(new Gt(new ArrayBuffer(1)))!=Br||Ht&&ze(new Ht)!=kr||Kt&&ze(Kt.resolve())!=Ir||qt&&ze(new qt)!=Er||Xt&&ze(new Xt)!=Ar)&&(ze=function(e){var t=Ur(e),r=t==ai?e.constructor:void 0,n=r?qe(r):"";if(n)switch(n){case ii:return Br;case li:return kr;case si:return Ir;case di:return Er;case ci:return Ar}return t});var ui="__lodash_hash_undefined__";function fi(e){return this.__data__.set(e,ui),this}function hi(e){return this.__data__.has(e)}function mt(e){var t=-1,r=e==null?0:e.length;for(this.__data__=new fo;++t<r;)this.add(e[t])}mt.prototype.add=mt.prototype.push=fi;mt.prototype.has=hi;function pi(e,t){for(var r=-1,n=e==null?0:e.length;++r<n;)if(t(e[r],r,e))return!0;return!1}function vi(e,t){return e.has(t)}var bi=1,gi=2;function bn(e,t,r,n,o,l){var s=r&bi,i=e.length,a=t.length;if(i!=a&&!(s&&a>i))return!1;var u=l.get(e),d=l.get(t);if(u&&d)return u==t&&d==e;var g=-1,f=!0,c=r&gi?new mt:void 0;for(l.set(e,t),l.set(t,e);++g<i;){var h=e[g],b=t[g];if(n)var m=s?n(b,h,g,t,e,l):n(h,b,g,e,t,l);if(m!==void 0){if(m)continue;f=!1;break}if(c){if(!pi(t,function(v,C){if(!vi(c,C)&&(h===v||o(h,v,r,n,l)))return c.push(C)})){f=!1;break}}else if(!(h===b||o(h,b,r,n,l))){f=!1;break}}return l.delete(e),l.delete(t),f}function mi(e){var t=-1,r=Array(e.size);return e.forEach(function(n,o){r[++t]=[o,n]}),r}function yi(e){var t=-1,r=Array(e.size);return e.forEach(function(n){r[++t]=n}),r}var xi=1,wi=2,Ci="[object Boolean]",$i="[object Date]",Si="[object Error]",_i="[object Map]",Pi="[object Number]",zi="[object RegExp]",Ti="[object Set]",ki="[object String]",Ii="[object Symbol]",Ei="[object ArrayBuffer]",Ai="[object DataView]",Mr=ur?ur.prototype:void 0,Bt=Mr?Mr.valueOf:void 0;function Bi(e,t,r,n,o,l,s){switch(r){case Ai:if(e.byteLength!=t.byteLength||e.byteOffset!=t.byteOffset)return!1;e=e.buffer,t=t.buffer;case Ei:return!(e.byteLength!=t.byteLength||!l(new fr(e),new fr(t)));case Ci:case $i:case Pi:return ho(+e,+t);case Si:return e.name==t.name&&e.message==t.message;case zi:case ki:return e==t+"";case _i:var i=mi;case Ti:var a=n&xi;if(i||(i=yi),e.size!=t.size&&!a)return!1;var u=s.get(e);if(u)return u==t;n|=wi,s.set(e,t);var d=bn(i(e),i(t),n,o,l,s);return s.delete(e),d;case Ii:if(Bt)return Bt.call(e)==Bt.call(t)}return!1}var Mi=1,Oi=Object.prototype,Li=Oi.hasOwnProperty;function Ri(e,t,r,n,o,l){var s=r&Mi,i=Tr(e),a=i.length,u=Tr(t),d=u.length;if(a!=d&&!s)return!1;for(var g=a;g--;){var f=i[g];if(!(s?f in t:Li.call(t,f)))return!1}var c=l.get(e),h=l.get(t);if(c&&h)return c==t&&h==e;var b=!0;l.set(e,t),l.set(t,e);for(var m=s;++g<a;){f=i[g];var v=e[f],C=t[f];if(n)var R=s?n(C,v,f,t,e,l):n(v,C,f,e,t,l);if(!(R===void 0?v===C||o(v,C,r,n,l):R)){b=!1;break}m||(m=f=="constructor")}if(b&&!m){var B=e.constructor,T=t.constructor;B!=T&&"constructor"in e&&"constructor"in t&&!(typeof B=="function"&&B instanceof B&&typeof T=="function"&&T instanceof T)&&(b=!1)}return l.delete(e),l.delete(t),b}var Fi=1,Or="[object Arguments]",Lr="[object Array]",ft="[object Object]",Wi=Object.prototype,Rr=Wi.hasOwnProperty;function Hi(e,t,r,n,o,l){var s=Xe(e),i=Xe(t),a=s?Lr:ze(e),u=i?Lr:ze(t);a=a==Or?ft:a,u=u==Or?ft:u;var d=a==ft,g=u==ft,f=a==u;if(f&&hr(e)){if(!hr(t))return!1;s=!0,d=!1}if(f&&!d)return l||(l=new vt),s||po(e)?bn(e,t,r,n,o,l):Bi(e,t,a,r,n,o,l);if(!(r&Fi)){var c=d&&Rr.call(e,"__wrapped__"),h=g&&Rr.call(t,"__wrapped__");if(c||h){var b=c?e.value():e,m=h?t.value():t;return l||(l=new vt),o(b,m,r,n,l)}}return f?(l||(l=new vt),Ri(e,t,r,n,o,l)):!1}function ir(e,t,r,n,o){return e===t?!0:e==null||t==null||!pr(e)&&!pr(t)?e!==e&&t!==t:Hi(e,t,r,n,ir,o)}var Di=1,ji=2;function Ni(e,t,r,n){var o=r.length,l=o;if(e==null)return!l;for(e=Object(e);o--;){var s=r[o];if(s[2]?s[1]!==e[s[0]]:!(s[0]in e))return!1}for(;++o<l;){s=r[o];var i=s[0],a=e[i],u=s[1];if(s[2]){if(a===void 0&&!(i in e))return!1}else{var d=new vt,g;if(!(g===void 0?ir(u,a,Di|ji,n,d):g))return!1}}return!0}function gn(e){return e===e&&!rt(e)}function Ui(e){for(var t=ar(e),r=t.length;r--;){var n=t[r],o=e[n];t[r]=[n,o,gn(o)]}return t}function mn(e,t){return function(r){return r==null?!1:r[e]===t&&(t!==void 0||e in Object(r))}}function Vi(e){var t=Ui(e);return t.length==1&&t[0][2]?mn(t[0][0],t[0][1]):function(r){return r===e||Ni(r,e,t)}}function Xi(e,t){return e!=null&&t in Object(e)}function Gi(e,t,r){t=vo(t,e);for(var n=-1,o=t.length,l=!1;++n<o;){var s=Qt(t[n]);if(!(l=e!=null&&r(e,s)))break;e=e[s]}return l||++n!=o?l:(o=e==null?0:e.length,!!o&&bo(o)&&go(s,o)&&(Xe(e)||mo(e)))}function Ki(e,t){return e!=null&&Gi(e,t,Xi)}var qi=1,Yi=2;function Zi(e,t){return Vr(e)&&gn(t)?mn(Qt(e),t):function(r){var n=yo(r,e);return n===void 0&&n===t?Ki(r,e):ir(t,n,qi|Yi)}}function Ji(e){return function(t){return t==null?void 0:t[e]}}function Qi(e){return function(t){return xo(t,e)}}function el(e){return Vr(e)?Ji(Qt(e)):Qi(e)}function tl(e){return typeof e=="function"?e:e==null?wo:typeof e=="object"?Xe(e)?Zi(e[0],e[1]):Vi(e):el(e)}function rl(e,t){return e&&Co(e,t,ar)}function nl(e,t){return function(r,n){if(r==null)return r;if(!Jt(r))return e(r,n);for(var o=r.length,l=-1,s=Object(r);++l<o&&n(s[l],l,s)!==!1;);return r}}var ol=nl(rl),Mt=function(){return at.Date.now()},al="Expected a function",il=Math.max,ll=Math.min;function sl(e,t,r){var n,o,l,s,i,a,u=0,d=!1,g=!1,f=!0;if(typeof e!="function")throw new TypeError(al);t=Pr(t)||0,rt(r)&&(d=!!r.leading,g="maxWait"in r,l=g?il(Pr(r.maxWait)||0,t):l,f="trailing"in r?!!r.trailing:f);function c(P){var S=n,D=o;return n=o=void 0,u=P,s=e.apply(D,S),s}function h(P){return u=P,i=setTimeout(v,t),d?c(P):s}function b(P){var S=P-a,D=P-u,j=t-S;return g?ll(j,l-D):j}function m(P){var S=P-a,D=P-u;return a===void 0||S>=t||S<0||g&&D>=l}function v(){var P=Mt();if(m(P))return C(P);i=setTimeout(v,b(P))}function C(P){return i=void 0,f&&n?c(P):(n=o=void 0,s)}function R(){i!==void 0&&clearTimeout(i),u=0,n=a=o=i=void 0}function B(){return i===void 0?s:C(Mt())}function T(){var P=Mt(),S=m(P);if(n=arguments,o=this,a=P,S){if(i===void 0)return h(a);if(g)return clearTimeout(i),i=setTimeout(v,t),c(a)}return i===void 0&&(i=setTimeout(v,t)),s}return T.cancel=R,T.flush=B,T}function dl(e,t){var r=-1,n=Jt(e)?Array(e.length):[];return ol(e,function(o,l,s){n[++r]=t(o,l,s)}),n}function cl(e,t){var r=Xe(e)?$o:dl;return r(e,tl(t))}var ul="Expected a function";function Ot(e,t,r){var n=!0,o=!0;if(typeof e!="function")throw new TypeError(ul);return rt(r)&&(n="leading"in r?!!r.leading:n,o="trailing"in r?!!r.trailing:o),sl(e,t,{leading:n,maxWait:t,trailing:o})}const fl=U({name:"Add",render(){return p("svg",{width:"512",height:"512",viewBox:"0 0 512 512",fill:"none",xmlns:"http://www.w3.org/2000/svg"},p("path",{d:"M256 112V400M400 256H112",stroke:"currentColor","stroke-width":"32","stroke-linecap":"round","stroke-linejoin":"round"}))}}),Lt={top:"bottom",bottom:"top",left:"right",right:"left"},J="var(--n-arrow-height) * 1.414",hl=M([y("popover",`
 transition:
 box-shadow .3s var(--n-bezier),
 background-color .3s var(--n-bezier),
 color .3s var(--n-bezier);
 position: relative;
 font-size: var(--n-font-size);
 color: var(--n-text-color);
 box-shadow: var(--n-box-shadow);
 word-break: break-word;
 `,[M(">",[y("scrollbar",`
 height: inherit;
 max-height: inherit;
 `)]),Te("raw",`
 background-color: var(--n-color);
 border-radius: var(--n-border-radius);
 `,[Te("scrollable",[Te("show-header-or-footer","padding: var(--n-padding);")])]),z("header",`
 padding: var(--n-padding);
 border-bottom: 1px solid var(--n-divider-color);
 transition: border-color .3s var(--n-bezier);
 `),z("footer",`
 padding: var(--n-padding);
 border-top: 1px solid var(--n-divider-color);
 transition: border-color .3s var(--n-bezier);
 `),$("scrollable, show-header-or-footer",[z("content",`
 padding: var(--n-padding);
 `)])]),y("popover-shared",`
 transform-origin: inherit;
 `,[y("popover-arrow-wrapper",`
 position: absolute;
 overflow: hidden;
 pointer-events: none;
 `,[y("popover-arrow",`
 transition: background-color .3s var(--n-bezier);
 position: absolute;
 display: block;
 width: calc(${J});
 height: calc(${J});
 box-shadow: 0 0 8px 0 rgba(0, 0, 0, .12);
 transform: rotate(45deg);
 background-color: var(--n-color);
 pointer-events: all;
 `)]),M("&.popover-transition-enter-from, &.popover-transition-leave-to",`
 opacity: 0;
 transform: scale(.85);
 `),M("&.popover-transition-enter-to, &.popover-transition-leave-from",`
 transform: scale(1);
 opacity: 1;
 `),M("&.popover-transition-enter-active",`
 transition:
 box-shadow .3s var(--n-bezier),
 background-color .3s var(--n-bezier),
 color .3s var(--n-bezier),
 opacity .15s var(--n-bezier-ease-out),
 transform .15s var(--n-bezier-ease-out);
 `),M("&.popover-transition-leave-active",`
 transition:
 box-shadow .3s var(--n-bezier),
 background-color .3s var(--n-bezier),
 color .3s var(--n-bezier),
 opacity .15s var(--n-bezier-ease-in),
 transform .15s var(--n-bezier-ease-in);
 `)]),ie("top-start",`
 top: calc(${J} / -2);
 left: calc(${ge("top-start")} - var(--v-offset-left));
 `),ie("top",`
 top: calc(${J} / -2);
 transform: translateX(calc(${J} / -2)) rotate(45deg);
 left: 50%;
 `),ie("top-end",`
 top: calc(${J} / -2);
 right: calc(${ge("top-end")} + var(--v-offset-left));
 `),ie("bottom-start",`
 bottom: calc(${J} / -2);
 left: calc(${ge("bottom-start")} - var(--v-offset-left));
 `),ie("bottom",`
 bottom: calc(${J} / -2);
 transform: translateX(calc(${J} / -2)) rotate(45deg);
 left: 50%;
 `),ie("bottom-end",`
 bottom: calc(${J} / -2);
 right: calc(${ge("bottom-end")} + var(--v-offset-left));
 `),ie("left-start",`
 left: calc(${J} / -2);
 top: calc(${ge("left-start")} - var(--v-offset-top));
 `),ie("left",`
 left: calc(${J} / -2);
 transform: translateY(calc(${J} / -2)) rotate(45deg);
 top: 50%;
 `),ie("left-end",`
 left: calc(${J} / -2);
 bottom: calc(${ge("left-end")} + var(--v-offset-top));
 `),ie("right-start",`
 right: calc(${J} / -2);
 top: calc(${ge("right-start")} - var(--v-offset-top));
 `),ie("right",`
 right: calc(${J} / -2);
 transform: translateY(calc(${J} / -2)) rotate(45deg);
 top: 50%;
 `),ie("right-end",`
 right: calc(${J} / -2);
 bottom: calc(${ge("right-end")} + var(--v-offset-top));
 `),...cl({top:["right-start","left-start"],right:["top-end","bottom-end"],bottom:["right-end","left-end"],left:["top-start","bottom-start"]},(e,t)=>{const r=["right","left"].includes(t),n=r?"width":"height";return e.map(o=>{const l=o.split("-")[1]==="end",i=`calc((${`var(--v-target-${n}, 0px)`} - ${J}) / 2)`,a=ge(o);return M(`[v-placement="${o}"] >`,[y("popover-shared",[$("center-arrow",[y("popover-arrow",`${t}: calc(max(${i}, ${a}) ${l?"+":"-"} var(--v-offset-${r?"left":"top"}));`)])])])})})]);function ge(e){return["top","bottom"].includes(e.split("-")[0])?"var(--n-arrow-offset)":"var(--n-arrow-offset-vertical)"}function ie(e,t){const r=e.split("-")[0],n=["top","bottom"].includes(r)?"height: var(--n-space-arrow);":"width: var(--n-space-arrow);";return M(`[v-placement="${e}"] >`,[y("popover-shared",`
 margin-${Lt[r]}: var(--n-space);
 `,[$("show-arrow",`
 margin-${Lt[r]}: var(--n-space-arrow);
 `),$("overlap",`
 margin: 0;
 `),So("popover-arrow-wrapper",`
 right: 0;
 left: 0;
 top: 0;
 bottom: 0;
 ${r}: 100%;
 ${Lt[r]}: auto;
 ${n}
 `,[y("popover-arrow",t)])])])}const yn=Object.assign(Object.assign({},re.props),{to:Ge.propTo,show:Boolean,trigger:String,showArrow:Boolean,delay:Number,duration:Number,raw:Boolean,arrowPointToCenter:Boolean,arrowClass:String,arrowStyle:[String,Object],arrowWrapperClass:String,arrowWrapperStyle:[String,Object],displayDirective:String,x:Number,y:Number,flip:Boolean,overlap:Boolean,placement:String,width:[Number,String],keepAliveOnHover:Boolean,scrollable:Boolean,contentClass:String,contentStyle:[Object,String],headerClass:String,headerStyle:[Object,String],footerClass:String,footerStyle:[Object,String],internalDeactivateImmediately:Boolean,animated:Boolean,onClickoutside:Function,internalTrapFocus:Boolean,internalOnAfterLeave:Function,minWidth:Number,maxWidth:Number});function pl({arrowClass:e,arrowStyle:t,arrowWrapperClass:r,arrowWrapperStyle:n,clsPrefix:o}){return p("div",{key:"__popover-arrow__",style:n,class:[`${o}-popover-arrow-wrapper`,r]},p("div",{class:[`${o}-popover-arrow`,e],style:t}))}const vl=U({name:"PopoverBody",inheritAttrs:!1,props:yn,setup(e,{slots:t,attrs:r}){const{namespaceRef:n,mergedClsPrefixRef:o,inlineThemeDisabled:l}=Ce(e),s=re("Popover","-popover",hl,Po,e,o),i=I(null),a=ae("NPopover"),u=I(null),d=I(e.show),g=I(!1);er(()=>{const{show:S}=e;S&&!Fa()&&!e.internalDeactivateImmediately&&(g.value=!0)});const f=K(()=>{const{trigger:S,onClickoutside:D}=e,j=[],{positionManuallyRef:{value:O}}=a;return O||(S==="click"&&!D&&j.push([yr,B,void 0,{capture:!0}]),S==="hover"&&j.push([Ca,R])),D&&j.push([yr,B,void 0,{capture:!0}]),(e.displayDirective==="show"||e.animated&&g.value)&&j.push([Xr,e.show]),j}),c=K(()=>{const{common:{cubicBezierEaseInOut:S,cubicBezierEaseIn:D,cubicBezierEaseOut:j},self:{space:O,spaceArrow:Z,padding:N,fontSize:X,textColor:ne,dividerColor:_,color:V,boxShadow:ee,borderRadius:he,arrowHeight:de,arrowOffset:oe,arrowOffsetVertical:Ze}}=s.value;return{"--n-box-shadow":ee,"--n-bezier":S,"--n-bezier-ease-in":D,"--n-bezier-ease-out":j,"--n-font-size":X,"--n-text-color":ne,"--n-color":V,"--n-divider-color":_,"--n-border-radius":he,"--n-arrow-height":de,"--n-arrow-offset":oe,"--n-arrow-offset-vertical":Ze,"--n-padding":N,"--n-space":O,"--n-space-arrow":Z}}),h=K(()=>{const S=e.width==="trigger"?void 0:Pt(e.width),D=[];S&&D.push({width:S});const{maxWidth:j,minWidth:O}=e;return j&&D.push({maxWidth:Pt(j)}),O&&D.push({maxWidth:Pt(O)}),l||D.push(c.value),D}),b=l?Oe("popover",void 0,c,e):void 0;a.setBodyInstance({syncPosition:m}),Ke(()=>{a.setBodyInstance(null)}),pe(G(e,"show"),S=>{e.animated||(S?d.value=!0:d.value=!1)});function m(){var S;(S=i.value)===null||S===void 0||S.syncPosition()}function v(S){e.trigger==="hover"&&e.keepAliveOnHover&&e.show&&a.handleMouseEnter(S)}function C(S){e.trigger==="hover"&&e.keepAliveOnHover&&a.handleMouseLeave(S)}function R(S){e.trigger==="hover"&&!T().contains(Wt(S))&&a.handleMouseMoveOutside(S)}function B(S){(e.trigger==="click"&&!T().contains(Wt(S))||e.onClickoutside)&&a.handleClickOutside(S)}function T(){return a.getTriggerElement()}me(on,u),me(rn,null),me(nn,null);function P(){if(b==null||b.onRender(),!(e.displayDirective==="show"||e.show||e.animated&&g.value))return null;let D;const j=a.internalRenderBodyRef.value,{value:O}=o;if(j)D=j([`${O}-popover-shared`,b==null?void 0:b.themeClass.value,e.overlap&&`${O}-popover-shared--overlap`,e.showArrow&&`${O}-popover-shared--show-arrow`,e.arrowPointToCenter&&`${O}-popover-shared--center-arrow`],u,h.value,v,C);else{const{value:Z}=a.extraClassRef,{internalTrapFocus:N}=e,X=!vr(t.header)||!vr(t.footer),ne=()=>{var _,V;const ee=X?p(Be,null,ye(t.header,oe=>oe?p("div",{class:[`${O}-popover__header`,e.headerClass],style:e.headerStyle},oe):null),ye(t.default,oe=>oe?p("div",{class:[`${O}-popover__content`,e.contentClass],style:e.contentStyle},t):null),ye(t.footer,oe=>oe?p("div",{class:[`${O}-popover__footer`,e.footerClass],style:e.footerStyle},oe):null)):e.scrollable?(_=t.default)===null||_===void 0?void 0:_.call(t):p("div",{class:[`${O}-popover__content`,e.contentClass],style:e.contentStyle},t),he=e.scrollable?p(zo,{contentClass:X?void 0:`${O}-popover__content ${(V=e.contentClass)!==null&&V!==void 0?V:""}`,contentStyle:X?void 0:e.contentStyle},{default:()=>ee}):ee,de=e.showArrow?pl({arrowClass:e.arrowClass,arrowStyle:e.arrowStyle,arrowWrapperClass:e.arrowWrapperClass,arrowWrapperStyle:e.arrowWrapperStyle,clsPrefix:O}):null;return[he,de]};D=p("div",tr({class:[`${O}-popover`,`${O}-popover-shared`,b==null?void 0:b.themeClass.value,Z.map(_=>`${O}-${_}`),{[`${O}-popover--scrollable`]:e.scrollable,[`${O}-popover--show-header-or-footer`]:X,[`${O}-popover--raw`]:e.raw,[`${O}-popover-shared--overlap`]:e.overlap,[`${O}-popover-shared--show-arrow`]:e.showArrow,[`${O}-popover-shared--center-arrow`]:e.arrowPointToCenter}],ref:u,style:h.value,onKeydown:a.handleKeydown,onMouseenter:v,onMouseleave:C},r),N?p(Ra,{active:e.show,autoFocus:!0},{default:ne}):ne())}return ot(D,f.value)}return{displayed:g,namespace:n,isMounted:a.isMountedRef,zIndex:a.zIndexRef,followerRef:i,adjustedTo:Ge(e),followerEnabled:d,renderContentNode:P}},render(){return p(Ba,{ref:"followerRef",zIndex:this.zIndex,show:this.show,enabled:this.followerEnabled,to:this.adjustedTo,x:this.x,y:this.y,flip:this.flip,placement:this.placement,containerClass:this.namespace,overlap:this.overlap,width:this.width==="trigger"?"target":void 0,teleportDisabled:this.adjustedTo===Ge.tdkey},{default:()=>this.animated?p(_o,{name:"popover-transition",appear:this.isMounted,onEnter:()=>{this.followerEnabled=!0},onAfterLeave:()=>{var e;(e=this.internalOnAfterLeave)===null||e===void 0||e.call(this),this.followerEnabled=!1,this.displayed=!1}},{default:this.renderContentNode}):this.renderContentNode()})}}),bl=Object.keys(yn),gl={focus:["onFocus","onBlur"],click:["onClick"],hover:["onMouseenter","onMouseleave"],manual:[],nested:["onFocus","onBlur","onMouseenter","onMouseleave","onClick"]};function ml(e,t,r){gl[t].forEach(n=>{e.props?e.props=Object.assign({},e.props):e.props={};const o=e.props[n],l=r[n];o?e.props[n]=(...s)=>{o(...s),l(...s)}:e.props[n]=l})}const xn={show:{type:Boolean,default:void 0},defaultShow:Boolean,showArrow:{type:Boolean,default:!0},trigger:{type:String,default:"hover"},delay:{type:Number,default:100},duration:{type:Number,default:100},raw:Boolean,placement:{type:String,default:"top"},x:Number,y:Number,arrowPointToCenter:Boolean,disabled:Boolean,getDisabled:Function,displayDirective:{type:String,default:"if"},arrowClass:String,arrowStyle:[String,Object],arrowWrapperClass:String,arrowWrapperStyle:[String,Object],flip:{type:Boolean,default:!0},animated:{type:Boolean,default:!0},width:{type:[Number,String],default:void 0},overlap:Boolean,keepAliveOnHover:{type:Boolean,default:!0},zIndex:Number,to:Ge.propTo,scrollable:Boolean,contentClass:String,contentStyle:[Object,String],headerClass:String,headerStyle:[Object,String],footerClass:String,footerStyle:[Object,String],onClickoutside:Function,"onUpdate:show":[Function,Array],onUpdateShow:[Function,Array],internalDeactivateImmediately:Boolean,internalSyncTargetWithParent:Boolean,internalInheritedEventHandlers:{type:Array,default:()=>[]},internalTrapFocus:Boolean,internalExtraClass:{type:Array,default:()=>[]},onShow:[Function,Array],onHide:[Function,Array],arrow:{type:Boolean,default:void 0},minWidth:Number,maxWidth:Number},yl=Object.assign(Object.assign(Object.assign({},re.props),xn),{internalOnAfterLeave:Function,internalRenderBody:Function}),xl=U({name:"Popover",inheritAttrs:!1,props:yl,__popover__:!0,setup(e){const t=Nr(),r=I(null),n=K(()=>e.show),o=I(e.defaultShow),l=Kr(n,o),s=tt(()=>e.disabled?!1:l.value),i=()=>{if(e.disabled)return!0;const{getDisabled:_}=e;return!!(_!=null&&_())},a=()=>i()?!1:l.value,u=Nt(e,["arrow","showArrow"]),d=K(()=>e.overlap?!1:u.value);let g=null;const f=I(null),c=I(null),h=tt(()=>e.x!==void 0&&e.y!==void 0);function b(_){const{"onUpdate:show":V,onUpdateShow:ee,onShow:he,onHide:de}=e;o.value=_,V&&fe(V,_),ee&&fe(ee,_),_&&he&&fe(he,!0),_&&de&&fe(de,!1)}function m(){g&&g.syncPosition()}function v(){const{value:_}=f;_&&(window.clearTimeout(_),f.value=null)}function C(){const{value:_}=c;_&&(window.clearTimeout(_),c.value=null)}function R(){const _=i();if(e.trigger==="focus"&&!_){if(a())return;b(!0)}}function B(){const _=i();if(e.trigger==="focus"&&!_){if(!a())return;b(!1)}}function T(){const _=i();if(e.trigger==="hover"&&!_){if(C(),f.value!==null||a())return;const V=()=>{b(!0),f.value=null},{delay:ee}=e;ee===0?V():f.value=window.setTimeout(V,ee)}}function P(){const _=i();if(e.trigger==="hover"&&!_){if(v(),c.value!==null||!a())return;const V=()=>{b(!1),c.value=null},{duration:ee}=e;ee===0?V():c.value=window.setTimeout(V,ee)}}function S(){P()}function D(_){var V;a()&&(e.trigger==="click"&&(v(),C(),b(!1)),(V=e.onClickoutside)===null||V===void 0||V.call(e,_))}function j(){if(e.trigger==="click"&&!i()){v(),C();const _=!a();b(_)}}function O(_){e.internalTrapFocus&&_.key==="Escape"&&(v(),C(),b(!1))}function Z(_){o.value=_}function N(){var _;return(_=r.value)===null||_===void 0?void 0:_.targetRef}function X(_){g=_}return me("NPopover",{getTriggerElement:N,handleKeydown:O,handleMouseEnter:T,handleMouseLeave:P,handleClickOutside:D,handleMouseMoveOutside:S,setBodyInstance:X,positionManuallyRef:h,isMountedRef:t,zIndexRef:G(e,"zIndex"),extraClassRef:G(e,"internalExtraClass"),internalRenderBodyRef:G(e,"internalRenderBody")}),er(()=>{l.value&&i()&&b(!1)}),{binderInstRef:r,positionManually:h,mergedShowConsideringDisabledProp:s,uncontrolledShow:o,mergedShowArrow:d,getMergedShow:a,setShow:Z,handleClick:j,handleMouseEnter:T,handleMouseLeave:P,handleFocus:R,handleBlur:B,syncPosition:m}},render(){var e;const{positionManually:t,$slots:r}=this;let n,o=!1;if(!t&&(r.activator?n=Sr(r,"activator"):n=Sr(r,"trigger"),n)){n=Gr(n),n=n.type===To?p("span",[n]):n;const l={onClick:this.handleClick,onMouseenter:this.handleMouseEnter,onMouseleave:this.handleMouseLeave,onFocus:this.handleFocus,onBlur:this.handleBlur};if(!((e=n.type)===null||e===void 0)&&e.__popover__)o=!0,n.props||(n.props={internalSyncTargetWithParent:!0,internalInheritedEventHandlers:[]}),n.props.internalSyncTargetWithParent=!0,n.props.internalInheritedEventHandlers?n.props.internalInheritedEventHandlers=[l,...n.props.internalInheritedEventHandlers]:n.props.internalInheritedEventHandlers=[l];else{const{internalInheritedEventHandlers:s}=this,i=[l,...s],a={onBlur:u=>{i.forEach(d=>{d.onBlur(u)})},onFocus:u=>{i.forEach(d=>{d.onFocus(u)})},onClick:u=>{i.forEach(d=>{d.onClick(u)})},onMouseenter:u=>{i.forEach(d=>{d.onMouseenter(u)})},onMouseleave:u=>{i.forEach(d=>{d.onMouseleave(u)})}};ml(n,s?"nested":t?"manual":this.trigger,a)}}return p(xa,{ref:"binderInstRef",syncTarget:!o,syncTargetWithParent:this.internalSyncTargetWithParent},{default:()=>{this.mergedShowConsideringDisabledProp;const l=this.getMergedShow();return[this.internalTrapFocus&&l?ot(p("div",{style:{position:"fixed",top:0,right:0,bottom:0,left:0}}),[[dn,{enabled:l,zIndex:this.zIndex}]]):null,t?null:p(wa,null,{default:()=>n}),p(vl,vn(this.$props,bl,Object.assign(Object.assign({},this.$attrs),{showArrow:this.mergedShowArrow,show:l})),{default:()=>{var s,i;return(i=(s=this.$slots).default)===null||i===void 0?void 0:i.call(s)},header:()=>{var s,i;return(i=(s=this.$slots).header)===null||i===void 0?void 0:i.call(s)},footer:()=>{var s,i;return(i=(s=this.$slots).footer)===null||i===void 0?void 0:i.call(s)}})]}})}});function wl(e){const{textColor2:t,primaryColorHover:r,primaryColorPressed:n,primaryColor:o,infoColor:l,successColor:s,warningColor:i,errorColor:a,baseColor:u,borderColor:d,opacityDisabled:g,tagColor:f,closeIconColor:c,closeIconColorHover:h,closeIconColorPressed:b,borderRadiusSmall:m,fontSizeMini:v,fontSizeTiny:C,fontSizeSmall:R,fontSizeMedium:B,heightMini:T,heightTiny:P,heightSmall:S,heightMedium:D,closeColorHover:j,closeColorPressed:O,buttonColor2Hover:Z,buttonColor2Pressed:N,fontWeightStrong:X}=e;return Object.assign(Object.assign({},ko),{closeBorderRadius:m,heightTiny:T,heightSmall:P,heightMedium:S,heightLarge:D,borderRadius:m,opacityDisabled:g,fontSizeTiny:v,fontSizeSmall:C,fontSizeMedium:R,fontSizeLarge:B,fontWeightStrong:X,textColorCheckable:t,textColorHoverCheckable:t,textColorPressedCheckable:t,textColorChecked:u,colorCheckable:"#0000",colorHoverCheckable:Z,colorPressedCheckable:N,colorChecked:o,colorCheckedHover:r,colorCheckedPressed:n,border:`1px solid ${d}`,textColor:t,color:f,colorBordered:"rgb(250, 250, 252)",closeIconColor:c,closeIconColorHover:h,closeIconColorPressed:b,closeColorHover:j,closeColorPressed:O,borderPrimary:`1px solid ${H(o,{alpha:.3})}`,textColorPrimary:o,colorPrimary:H(o,{alpha:.12}),colorBorderedPrimary:H(o,{alpha:.1}),closeIconColorPrimary:o,closeIconColorHoverPrimary:o,closeIconColorPressedPrimary:o,closeColorHoverPrimary:H(o,{alpha:.12}),closeColorPressedPrimary:H(o,{alpha:.18}),borderInfo:`1px solid ${H(l,{alpha:.3})}`,textColorInfo:l,colorInfo:H(l,{alpha:.12}),colorBorderedInfo:H(l,{alpha:.1}),closeIconColorInfo:l,closeIconColorHoverInfo:l,closeIconColorPressedInfo:l,closeColorHoverInfo:H(l,{alpha:.12}),closeColorPressedInfo:H(l,{alpha:.18}),borderSuccess:`1px solid ${H(s,{alpha:.3})}`,textColorSuccess:s,colorSuccess:H(s,{alpha:.12}),colorBorderedSuccess:H(s,{alpha:.1}),closeIconColorSuccess:s,closeIconColorHoverSuccess:s,closeIconColorPressedSuccess:s,closeColorHoverSuccess:H(s,{alpha:.12}),closeColorPressedSuccess:H(s,{alpha:.18}),borderWarning:`1px solid ${H(i,{alpha:.35})}`,textColorWarning:i,colorWarning:H(i,{alpha:.15}),colorBorderedWarning:H(i,{alpha:.12}),closeIconColorWarning:i,closeIconColorHoverWarning:i,closeIconColorPressedWarning:i,closeColorHoverWarning:H(i,{alpha:.12}),closeColorPressedWarning:H(i,{alpha:.18}),borderError:`1px solid ${H(a,{alpha:.23})}`,textColorError:a,colorError:H(a,{alpha:.1}),colorBorderedError:H(a,{alpha:.08}),closeIconColorError:a,closeIconColorHoverError:a,closeIconColorPressedError:a,closeColorHoverError:H(a,{alpha:.12}),closeColorPressedError:H(a,{alpha:.18})})}const Cl={common:qr,self:wl},$l={color:Object,type:{type:String,default:"default"},round:Boolean,size:{type:String,default:"medium"},closable:Boolean,disabled:{type:Boolean,default:void 0}},Sl=y("tag",`
 --n-close-margin: var(--n-close-margin-top) var(--n-close-margin-right) var(--n-close-margin-bottom) var(--n-close-margin-left);
 white-space: nowrap;
 position: relative;
 box-sizing: border-box;
 cursor: default;
 display: inline-flex;
 align-items: center;
 flex-wrap: nowrap;
 padding: var(--n-padding);
 border-radius: var(--n-border-radius);
 color: var(--n-text-color);
 background-color: var(--n-color);
 transition: 
 border-color .3s var(--n-bezier),
 background-color .3s var(--n-bezier),
 color .3s var(--n-bezier),
 box-shadow .3s var(--n-bezier),
 opacity .3s var(--n-bezier);
 line-height: 1;
 height: var(--n-height);
 font-size: var(--n-font-size);
`,[$("strong",`
 font-weight: var(--n-font-weight-strong);
 `),z("border",`
 pointer-events: none;
 position: absolute;
 left: 0;
 right: 0;
 top: 0;
 bottom: 0;
 border-radius: inherit;
 border: var(--n-border);
 transition: border-color .3s var(--n-bezier);
 `),z("icon",`
 display: flex;
 margin: 0 4px 0 0;
 color: var(--n-text-color);
 transition: color .3s var(--n-bezier);
 font-size: var(--n-avatar-size-override);
 `),z("avatar",`
 display: flex;
 margin: 0 6px 0 0;
 `),z("close",`
 margin: var(--n-close-margin);
 transition:
 background-color .3s var(--n-bezier),
 color .3s var(--n-bezier);
 `),$("round",`
 padding: 0 calc(var(--n-height) / 3);
 border-radius: calc(var(--n-height) / 2);
 `,[z("icon",`
 margin: 0 4px 0 calc((var(--n-height) - 8px) / -2);
 `),z("avatar",`
 margin: 0 6px 0 calc((var(--n-height) - 8px) / -2);
 `),$("closable",`
 padding: 0 calc(var(--n-height) / 4) 0 calc(var(--n-height) / 3);
 `)]),$("icon, avatar",[$("round",`
 padding: 0 calc(var(--n-height) / 3) 0 calc(var(--n-height) / 2);
 `)]),$("disabled",`
 cursor: not-allowed !important;
 opacity: var(--n-opacity-disabled);
 `),$("checkable",`
 cursor: pointer;
 box-shadow: none;
 color: var(--n-text-color-checkable);
 background-color: var(--n-color-checkable);
 `,[Te("disabled",[M("&:hover","background-color: var(--n-color-hover-checkable);",[Te("checked","color: var(--n-text-color-hover-checkable);")]),M("&:active","background-color: var(--n-color-pressed-checkable);",[Te("checked","color: var(--n-text-color-pressed-checkable);")])]),$("checked",`
 color: var(--n-text-color-checked);
 background-color: var(--n-color-checked);
 `,[Te("disabled",[M("&:hover","background-color: var(--n-color-checked-hover);"),M("&:active","background-color: var(--n-color-checked-pressed);")])])])]),_l=Object.assign(Object.assign(Object.assign({},re.props),$l),{bordered:{type:Boolean,default:void 0},checked:Boolean,checkable:Boolean,strong:Boolean,triggerClickOnClose:Boolean,onClose:[Array,Function],onMouseenter:Function,onMouseleave:Function,"onUpdate:checked":Function,onUpdateChecked:Function,internalCloseFocusable:{type:Boolean,default:!0},internalCloseIsButtonTag:{type:Boolean,default:!0},onCheckedChange:Function}),Pl=Ae("n-tag"),zl=U({name:"Tag",props:_l,setup(e){const t=I(null),{mergedBorderedRef:r,mergedClsPrefixRef:n,inlineThemeDisabled:o,mergedRtlRef:l}=Ce(e),s=re("Tag","-tag",Sl,Cl,e,n);me(Pl,{roundRef:G(e,"round")});function i(){if(!e.disabled&&e.checkable){const{checked:c,onCheckedChange:h,onUpdateChecked:b,"onUpdate:checked":m}=e;b&&b(!c),m&&m(!c),h&&h(!c)}}function a(c){if(e.triggerClickOnClose||c.stopPropagation(),!e.disabled){const{onClose:h}=e;h&&fe(h,c)}}const u={setTextContent(c){const{value:h}=t;h&&(h.textContent=c)}},d=it("Tag",l,n),g=K(()=>{const{type:c,size:h,color:{color:b,textColor:m}={}}=e,{common:{cubicBezierEaseInOut:v},self:{padding:C,closeMargin:R,borderRadius:B,opacityDisabled:T,textColorCheckable:P,textColorHoverCheckable:S,textColorPressedCheckable:D,textColorChecked:j,colorCheckable:O,colorHoverCheckable:Z,colorPressedCheckable:N,colorChecked:X,colorCheckedHover:ne,colorCheckedPressed:_,closeBorderRadius:V,fontWeightStrong:ee,[W("colorBordered",c)]:he,[W("closeSize",h)]:de,[W("closeIconSize",h)]:oe,[W("fontSize",h)]:Ze,[W("height",h)]:lt,[W("color",c)]:Ct,[W("textColor",c)]:st,[W("border",c)]:$e,[W("closeIconColor",c)]:Le,[W("closeIconColorHover",c)]:dt,[W("closeIconColorPressed",c)]:$t,[W("closeColorHover",c)]:St,[W("closeColorPressed",c)]:Se}}=s.value,Re=Ne(R);return{"--n-font-weight-strong":ee,"--n-avatar-size-override":`calc(${lt} - 8px)`,"--n-bezier":v,"--n-border-radius":B,"--n-border":$e,"--n-close-icon-size":oe,"--n-close-color-pressed":Se,"--n-close-color-hover":St,"--n-close-border-radius":V,"--n-close-icon-color":Le,"--n-close-icon-color-hover":dt,"--n-close-icon-color-pressed":$t,"--n-close-icon-color-disabled":Le,"--n-close-margin-top":Re.top,"--n-close-margin-right":Re.right,"--n-close-margin-bottom":Re.bottom,"--n-close-margin-left":Re.left,"--n-close-size":de,"--n-color":b||(r.value?he:Ct),"--n-color-checkable":O,"--n-color-checked":X,"--n-color-checked-hover":ne,"--n-color-checked-pressed":_,"--n-color-hover-checkable":Z,"--n-color-pressed-checkable":N,"--n-font-size":Ze,"--n-height":lt,"--n-opacity-disabled":T,"--n-padding":C,"--n-text-color":m||st,"--n-text-color-checkable":P,"--n-text-color-checked":j,"--n-text-color-hover-checkable":S,"--n-text-color-pressed-checkable":D}}),f=o?Oe("tag",K(()=>{let c="";const{type:h,size:b,color:{color:m,textColor:v}={}}=e;return c+=h[0],c+=b[0],m&&(c+=`a${br(m)}`),v&&(c+=`b${br(v)}`),r.value&&(c+="c"),c}),g,e):void 0;return Object.assign(Object.assign({},u),{rtlEnabled:d,mergedClsPrefix:n,contentRef:t,mergedBordered:r,handleClick:i,handleCloseClick:a,cssVars:o?void 0:g,themeClass:f==null?void 0:f.themeClass,onRender:f==null?void 0:f.onRender})},render(){var e,t;const{mergedClsPrefix:r,rtlEnabled:n,closable:o,color:{borderColor:l}={},round:s,onRender:i,$slots:a}=this;i==null||i();const u=ye(a.avatar,g=>g&&p("div",{class:`${r}-tag__avatar`},g)),d=ye(a.icon,g=>g&&p("div",{class:`${r}-tag__icon`},g));return p("div",{class:[`${r}-tag`,this.themeClass,{[`${r}-tag--rtl`]:n,[`${r}-tag--strong`]:this.strong,[`${r}-tag--disabled`]:this.disabled,[`${r}-tag--checkable`]:this.checkable,[`${r}-tag--checked`]:this.checkable&&this.checked,[`${r}-tag--round`]:s,[`${r}-tag--avatar`]:u,[`${r}-tag--icon`]:d,[`${r}-tag--closable`]:o}],style:this.cssVars,onClick:this.handleClick,onMouseenter:this.onMouseenter,onMouseleave:this.onMouseleave},d||u,p("span",{class:`${r}-tag__content`,ref:"contentRef"},(t=(e=this.$slots).default)===null||t===void 0?void 0:t.call(e)),!this.checkable&&o?p(rr,{clsPrefix:r,class:`${r}-tag__close`,disabled:this.disabled,onClick:this.handleCloseClick,focusable:this.internalCloseFocusable,round:s,isButtonTag:this.internalCloseIsButtonTag,absolute:!0}):null,!this.checkable&&this.mergedBordered?p("div",{class:`${r}-tag__border`,style:{borderColor:l}}):null)}});function Tl(e){const{lineHeight:t,borderRadius:r,fontWeightStrong:n,baseColor:o,dividerColor:l,actionColor:s,textColor1:i,textColor2:a,closeColorHover:u,closeColorPressed:d,closeIconColor:g,closeIconColorHover:f,closeIconColorPressed:c,infoColor:h,successColor:b,warningColor:m,errorColor:v,fontSize:C}=e;return Object.assign(Object.assign({},Io),{fontSize:C,lineHeight:t,titleFontWeight:n,borderRadius:r,border:`1px solid ${l}`,color:s,titleTextColor:i,iconColor:a,contentTextColor:a,closeBorderRadius:r,closeColorHover:u,closeColorPressed:d,closeIconColor:g,closeIconColorHover:f,closeIconColorPressed:c,borderInfo:`1px solid ${_e(o,H(h,{alpha:.25}))}`,colorInfo:_e(o,H(h,{alpha:.08})),titleTextColorInfo:i,iconColorInfo:h,contentTextColorInfo:a,closeColorHoverInfo:u,closeColorPressedInfo:d,closeIconColorInfo:g,closeIconColorHoverInfo:f,closeIconColorPressedInfo:c,borderSuccess:`1px solid ${_e(o,H(b,{alpha:.25}))}`,colorSuccess:_e(o,H(b,{alpha:.08})),titleTextColorSuccess:i,iconColorSuccess:b,contentTextColorSuccess:a,closeColorHoverSuccess:u,closeColorPressedSuccess:d,closeIconColorSuccess:g,closeIconColorHoverSuccess:f,closeIconColorPressedSuccess:c,borderWarning:`1px solid ${_e(o,H(m,{alpha:.33}))}`,colorWarning:_e(o,H(m,{alpha:.08})),titleTextColorWarning:i,iconColorWarning:m,contentTextColorWarning:a,closeColorHoverWarning:u,closeColorPressedWarning:d,closeIconColorWarning:g,closeIconColorHoverWarning:f,closeIconColorPressedWarning:c,borderError:`1px solid ${_e(o,H(v,{alpha:.25}))}`,colorError:_e(o,H(v,{alpha:.08})),titleTextColorError:i,iconColorError:v,contentTextColorError:a,closeColorHoverError:u,closeColorPressedError:d,closeIconColorError:g,closeIconColorHoverError:f,closeIconColorPressedError:c})}const kl={common:qr,self:Tl},Il=y("alert",`
 line-height: var(--n-line-height);
 border-radius: var(--n-border-radius);
 position: relative;
 transition: background-color .3s var(--n-bezier);
 background-color: var(--n-color);
 text-align: start;
 word-break: break-word;
`,[z("border",`
 border-radius: inherit;
 position: absolute;
 left: 0;
 right: 0;
 top: 0;
 bottom: 0;
 transition: border-color .3s var(--n-bezier);
 border: var(--n-border);
 pointer-events: none;
 `),$("closable",[y("alert-body",[z("title",`
 padding-right: 24px;
 `)])]),z("icon",{color:"var(--n-icon-color)"}),y("alert-body",{padding:"var(--n-padding)"},[z("title",{color:"var(--n-title-text-color)"}),z("content",{color:"var(--n-content-text-color)"})]),Eo({originalTransition:"transform .3s var(--n-bezier)",enterToProps:{transform:"scale(1)"},leaveToProps:{transform:"scale(0.9)"}}),z("icon",`
 position: absolute;
 left: 0;
 top: 0;
 align-items: center;
 justify-content: center;
 display: flex;
 width: var(--n-icon-size);
 height: var(--n-icon-size);
 font-size: var(--n-icon-size);
 margin: var(--n-icon-margin);
 `),z("close",`
 transition:
 color .3s var(--n-bezier),
 background-color .3s var(--n-bezier);
 position: absolute;
 right: 0;
 top: 0;
 margin: var(--n-close-margin);
 `),$("show-icon",[y("alert-body",{paddingLeft:"calc(var(--n-icon-margin-left) + var(--n-icon-size) + var(--n-icon-margin-right))"})]),$("right-adjust",[y("alert-body",{paddingRight:"calc(var(--n-close-size) + var(--n-padding) + 2px)"})]),y("alert-body",`
 border-radius: var(--n-border-radius);
 transition: border-color .3s var(--n-bezier);
 `,[z("title",`
 transition: color .3s var(--n-bezier);
 font-size: 16px;
 line-height: 19px;
 font-weight: var(--n-title-font-weight);
 `,[M("& +",[z("content",{marginTop:"9px"})])]),z("content",{transition:"color .3s var(--n-bezier)",fontSize:"var(--n-font-size)"})]),z("icon",{transition:"color .3s var(--n-bezier)"})]),El=Object.assign(Object.assign({},re.props),{title:String,showIcon:{type:Boolean,default:!0},type:{type:String,default:"default"},bordered:{type:Boolean,default:!0},closable:Boolean,onClose:Function,onAfterLeave:Function,onAfterHide:Function}),Al=U({name:"Alert",inheritAttrs:!1,props:El,setup(e){const{mergedClsPrefixRef:t,mergedBorderedRef:r,inlineThemeDisabled:n,mergedRtlRef:o}=Ce(e),l=re("Alert","-alert",Il,kl,e,t),s=it("Alert",o,t),i=K(()=>{const{common:{cubicBezierEaseInOut:c},self:h}=l.value,{fontSize:b,borderRadius:m,titleFontWeight:v,lineHeight:C,iconSize:R,iconMargin:B,iconMarginRtl:T,closeIconSize:P,closeBorderRadius:S,closeSize:D,closeMargin:j,closeMarginRtl:O,padding:Z}=h,{type:N}=e,{left:X,right:ne}=Ne(B);return{"--n-bezier":c,"--n-color":h[W("color",N)],"--n-close-icon-size":P,"--n-close-border-radius":S,"--n-close-color-hover":h[W("closeColorHover",N)],"--n-close-color-pressed":h[W("closeColorPressed",N)],"--n-close-icon-color":h[W("closeIconColor",N)],"--n-close-icon-color-hover":h[W("closeIconColorHover",N)],"--n-close-icon-color-pressed":h[W("closeIconColorPressed",N)],"--n-icon-color":h[W("iconColor",N)],"--n-border":h[W("border",N)],"--n-title-text-color":h[W("titleTextColor",N)],"--n-content-text-color":h[W("contentTextColor",N)],"--n-line-height":C,"--n-border-radius":m,"--n-font-size":b,"--n-title-font-weight":v,"--n-icon-size":R,"--n-icon-margin":B,"--n-icon-margin-rtl":T,"--n-close-size":D,"--n-close-margin":j,"--n-close-margin-rtl":O,"--n-padding":Z,"--n-icon-margin-left":X,"--n-icon-margin-right":ne}}),a=n?Oe("alert",K(()=>e.type[0]),i,e):void 0,u=I(!0),d=()=>{const{onAfterLeave:c,onAfterHide:h}=e;c&&c(),h&&h()};return{rtlEnabled:s,mergedClsPrefix:t,mergedBordered:r,visible:u,handleCloseClick:()=>{var c;Promise.resolve((c=e.onClose)===null||c===void 0?void 0:c.call(e)).then(h=>{h!==!1&&(u.value=!1)})},handleAfterLeave:()=>{d()},mergedTheme:l,cssVars:n?void 0:i,themeClass:a==null?void 0:a.themeClass,onRender:a==null?void 0:a.onRender}},render(){var e;return(e=this.onRender)===null||e===void 0||e.call(this),p(Ao,{onAfterLeave:this.handleAfterLeave},{default:()=>{const{mergedClsPrefix:t,$slots:r}=this,n={class:[`${t}-alert`,this.themeClass,this.closable&&`${t}-alert--closable`,this.showIcon&&`${t}-alert--show-icon`,!this.title&&this.closable&&`${t}-alert--right-adjust`,this.rtlEnabled&&`${t}-alert--rtl`],style:this.cssVars,role:"alert"};return this.visible?p("div",Object.assign({},tr(this.$attrs,n)),this.closable&&p(rr,{clsPrefix:t,class:`${t}-alert__close`,onClick:this.handleCloseClick}),this.bordered&&p("div",{class:`${t}-alert__border`}),this.showIcon&&p("div",{class:`${t}-alert__icon`,"aria-hidden":"true"},Dt(r.icon,()=>[p(nr,{clsPrefix:t},{default:()=>{switch(this.type){case"success":return p(Oo,null);case"info":return p(Mo,null);case"warning":return p(Yr,null);case"error":return p(Bo,null);default:return null}}})])),p("div",{class:[`${t}-alert-body`,this.mergedBordered&&`${t}-alert-body--bordered`]},ye(r.header,o=>{const l=o||this.title;return l?p("div",{class:`${t}-alert-body__title`},l):null}),r.default&&p("div",{class:`${t}-alert-body__content`},r))):null}})}});function wn(){const e=ae(Lo,null);return e===null&&or("use-message","No outer <n-message-provider /> founded. See prerequisite in https://www.naiveui.com/en-US/os-theme/components/message for more details. If you want to use `useMessage` outside setup, please check https://www.naiveui.com/zh-CN/os-theme/components/message#Q-&-A."),e}function Bl(){return Ro}const Ml={self:Bl};let Rt;function Ol(){if(!Fo)return!0;if(Rt===void 0){const e=document.createElement("div");e.style.display="flex",e.style.flexDirection="column",e.style.rowGap="1px",e.appendChild(document.createElement("div")),e.appendChild(document.createElement("div")),document.body.appendChild(e);const t=e.scrollHeight===1;return document.body.removeChild(e),Rt=t}return Rt}const Ll=Object.assign(Object.assign({},re.props),{align:String,justify:{type:String,default:"start"},inline:Boolean,vertical:Boolean,reverse:Boolean,size:{type:[String,Number,Array],default:"medium"},wrapItem:{type:Boolean,default:!0},itemClass:String,itemStyle:[String,Object],wrap:{type:Boolean,default:!0},internalUseGap:{type:Boolean,default:void 0}}),Rl=U({name:"Space",props:Ll,setup(e){const{mergedClsPrefixRef:t,mergedRtlRef:r}=Ce(e),n=re("Space","-space",void 0,Ml,e,t),o=it("Space",r,t);return{useGap:Ol(),rtlEnabled:o,mergedClsPrefix:t,margin:K(()=>{const{size:l}=e;if(Array.isArray(l))return{horizontal:l[0],vertical:l[1]};if(typeof l=="number")return{horizontal:l,vertical:l};const{self:{[W("gap",l)]:s}}=n.value,{row:i,col:a}=Wo(s);return{horizontal:jt(a),vertical:jt(i)}})}},render(){const{vertical:e,reverse:t,align:r,inline:n,justify:o,itemClass:l,itemStyle:s,margin:i,wrap:a,mergedClsPrefix:u,rtlEnabled:d,useGap:g,wrapItem:f,internalUseGap:c}=this,h=Me(Wa(this),!1);if(!h.length)return null;const b=`${i.horizontal}px`,m=`${i.horizontal/2}px`,v=`${i.vertical}px`,C=`${i.vertical/2}px`,R=h.length-1,B=o.startsWith("space-");return p("div",{role:"none",class:[`${u}-space`,d&&`${u}-space--rtl`],style:{display:n?"inline-flex":"flex",flexDirection:e&&!t?"column":e&&t?"column-reverse":!e&&t?"row-reverse":"row",justifyContent:["start","end"].includes(o)?`flex-${o}`:o,flexWrap:!a||e?"nowrap":"wrap",marginTop:g||e?"":`-${C}`,marginBottom:g||e?"":`-${C}`,alignItems:r,gap:g?`${i.vertical}px ${i.horizontal}px`:""}},!f&&(g||c)?h:h.map((T,P)=>T.type===Zt?T:p("div",{role:"none",class:l,style:[s,{maxWidth:"100%"},g?"":e?{marginBottom:P!==R?v:""}:d?{marginLeft:B?o==="space-between"&&P===R?"":m:P!==R?b:"",marginRight:B?o==="space-between"&&P===0?"":m:"",paddingTop:C,paddingBottom:C}:{marginRight:B?o==="space-between"&&P===R?"":m:P!==R?b:"",marginLeft:B?o==="space-between"&&P===0?"":m:"",paddingTop:C,paddingBottom:C}]},T)))}}),Fl=M([y("list",`
 --n-merged-border-color: var(--n-border-color);
 --n-merged-color: var(--n-color);
 --n-merged-color-hover: var(--n-color-hover);
 margin: 0;
 font-size: var(--n-font-size);
 transition:
 background-color .3s var(--n-bezier),
 color .3s var(--n-bezier),
 border-color .3s var(--n-bezier);
 padding: 0;
 list-style-type: none;
 color: var(--n-text-color);
 background-color: var(--n-merged-color);
 `,[$("show-divider",[y("list-item",[M("&:not(:last-child)",[z("divider",`
 background-color: var(--n-merged-border-color);
 `)])])]),$("clickable",[y("list-item",`
 cursor: pointer;
 `)]),$("bordered",`
 border: 1px solid var(--n-merged-border-color);
 border-radius: var(--n-border-radius);
 `),$("hoverable",[y("list-item",`
 border-radius: var(--n-border-radius);
 `,[M("&:hover",`
 background-color: var(--n-merged-color-hover);
 `,[z("divider",`
 background-color: transparent;
 `)])])]),$("bordered, hoverable",[y("list-item",`
 padding: 12px 20px;
 `),z("header, footer",`
 padding: 12px 20px;
 `)]),z("header, footer",`
 padding: 12px 0;
 box-sizing: border-box;
 transition: border-color .3s var(--n-bezier);
 `,[M("&:not(:last-child)",`
 border-bottom: 1px solid var(--n-merged-border-color);
 `)]),y("list-item",`
 position: relative;
 padding: 12px 0; 
 box-sizing: border-box;
 display: flex;
 flex-wrap: nowrap;
 align-items: center;
 transition:
 background-color .3s var(--n-bezier),
 border-color .3s var(--n-bezier);
 `,[z("prefix",`
 margin-right: 20px;
 flex: 0;
 `),z("suffix",`
 margin-left: 20px;
 flex: 0;
 `),z("main",`
 flex: 1;
 `),z("divider",`
 height: 1px;
 position: absolute;
 bottom: 0;
 left: 0;
 right: 0;
 background-color: transparent;
 transition: background-color .3s var(--n-bezier);
 pointer-events: none;
 `)])]),Ho(y("list",`
 --n-merged-color-hover: var(--n-color-hover-modal);
 --n-merged-color: var(--n-color-modal);
 --n-merged-border-color: var(--n-border-color-modal);
 `)),Do(y("list",`
 --n-merged-color-hover: var(--n-color-hover-popover);
 --n-merged-color: var(--n-color-popover);
 --n-merged-border-color: var(--n-border-color-popover);
 `))]),Wl=Object.assign(Object.assign({},re.props),{size:{type:String,default:"medium"},bordered:Boolean,clickable:Boolean,hoverable:Boolean,showDivider:{type:Boolean,default:!0}}),Cn=Ae("n-list"),$n=U({name:"List",props:Wl,setup(e){const{mergedClsPrefixRef:t,inlineThemeDisabled:r,mergedRtlRef:n}=Ce(e),o=it("List",n,t),l=re("List","-list",Fl,jo,e,t);me(Cn,{showDividerRef:G(e,"showDivider"),mergedClsPrefixRef:t});const s=K(()=>{const{common:{cubicBezierEaseInOut:a},self:{fontSize:u,textColor:d,color:g,colorModal:f,colorPopover:c,borderColor:h,borderColorModal:b,borderColorPopover:m,borderRadius:v,colorHover:C,colorHoverModal:R,colorHoverPopover:B}}=l.value;return{"--n-font-size":u,"--n-bezier":a,"--n-text-color":d,"--n-color":g,"--n-border-radius":v,"--n-border-color":h,"--n-border-color-modal":b,"--n-border-color-popover":m,"--n-color-modal":f,"--n-color-popover":c,"--n-color-hover":C,"--n-color-hover-modal":R,"--n-color-hover-popover":B}}),i=r?Oe("list",void 0,s,e):void 0;return{mergedClsPrefix:t,rtlEnabled:o,cssVars:r?void 0:s,themeClass:i==null?void 0:i.themeClass,onRender:i==null?void 0:i.onRender}},render(){var e;const{$slots:t,mergedClsPrefix:r,onRender:n}=this;return n==null||n(),p("ul",{class:[`${r}-list`,this.rtlEnabled&&`${r}-list--rtl`,this.bordered&&`${r}-list--bordered`,this.showDivider&&`${r}-list--show-divider`,this.hoverable&&`${r}-list--hoverable`,this.clickable&&`${r}-list--clickable`,this.themeClass],style:this.cssVars},t.header?p("div",{class:`${r}-list__header`},t.header()):null,(e=t.default)===null||e===void 0?void 0:e.call(t),t.footer?p("div",{class:`${r}-list__footer`},t.footer()):null)}}),Sn=U({name:"ListItem",setup(){const e=ae(Cn,null);return e||or("list-item","`n-list-item` must be placed in `n-list`."),{showDivider:e.showDividerRef,mergedClsPrefix:e.mergedClsPrefixRef}},render(){const{$slots:e,mergedClsPrefix:t}=this;return p("li",{class:`${t}-list-item`},e.prefix?p("div",{class:`${t}-list-item__prefix`},e.prefix()):null,e.default?p("div",{class:`${t}-list-item__main`},e):null,e.suffix?p("div",{class:`${t}-list-item__suffix`},e.suffix()):null,this.showDivider&&p("div",{class:`${t}-list-item__divider`}))}}),_n=Ae("n-popconfirm"),Pn={positiveText:String,negativeText:String,showIcon:{type:Boolean,default:!0},onPositiveClick:{type:Function,required:!0},onNegativeClick:{type:Function,required:!0}},Fr=No(Pn),Hl=U({name:"NPopconfirmPanel",props:Pn,setup(e){const{localeRef:t}=gr("Popconfirm"),{inlineThemeDisabled:r}=Ce(),{mergedClsPrefixRef:n,mergedThemeRef:o,props:l}=ae(_n),s=K(()=>{const{common:{cubicBezierEaseInOut:a},self:{fontSize:u,iconSize:d,iconColor:g}}=o.value;return{"--n-bezier":a,"--n-font-size":u,"--n-icon-size":d,"--n-icon-color":g}}),i=r?Oe("popconfirm-panel",void 0,s,l):void 0;return Object.assign(Object.assign({},gr("Popconfirm")),{mergedClsPrefix:n,cssVars:r?void 0:s,localizedPositiveText:K(()=>e.positiveText||t.value.positiveText),localizedNegativeText:K(()=>e.negativeText||t.value.negativeText),positiveButtonProps:G(l,"positiveButtonProps"),negativeButtonProps:G(l,"negativeButtonProps"),handlePositiveClick(a){e.onPositiveClick(a)},handleNegativeClick(a){e.onNegativeClick(a)},themeClass:i==null?void 0:i.themeClass,onRender:i==null?void 0:i.onRender})},render(){var e;const{mergedClsPrefix:t,showIcon:r,$slots:n}=this,o=Dt(n.action,()=>this.negativeText===null&&this.positiveText===null?[]:[this.negativeText!==null&&p(xe,Object.assign({size:"small",onClick:this.handleNegativeClick},this.negativeButtonProps),{default:()=>this.localizedNegativeText}),this.positiveText!==null&&p(xe,Object.assign({size:"small",type:"primary",onClick:this.handlePositiveClick},this.positiveButtonProps),{default:()=>this.localizedPositiveText})]);return(e=this.onRender)===null||e===void 0||e.call(this),p("div",{class:[`${t}-popconfirm__panel`,this.themeClass],style:this.cssVars},ye(n.default,l=>r||l?p("div",{class:`${t}-popconfirm__body`},r?p("div",{class:`${t}-popconfirm__icon`},Dt(n.icon,()=>[p(nr,{clsPrefix:t},{default:()=>p(Yr,null)})])):null,l):null),o?p("div",{class:[`${t}-popconfirm__action`]},o):null)}}),Dl=y("popconfirm",[z("body",`
 font-size: var(--n-font-size);
 display: flex;
 align-items: center;
 flex-wrap: nowrap;
 position: relative;
 `,[z("icon",`
 display: flex;
 font-size: var(--n-icon-size);
 color: var(--n-icon-color);
 transition: color .3s var(--n-bezier);
 margin: 0 8px 0 0;
 `)]),z("action",`
 display: flex;
 justify-content: flex-end;
 `,[M("&:not(:first-child)","margin-top: 8px"),y("button",[M("&:not(:last-child)","margin-right: 8px;")])])]),jl=Object.assign(Object.assign(Object.assign({},re.props),xn),{positiveText:String,negativeText:String,showIcon:{type:Boolean,default:!0},trigger:{type:String,default:"click"},positiveButtonProps:Object,negativeButtonProps:Object,onPositiveClick:Function,onNegativeClick:Function}),yt=U({name:"Popconfirm",props:jl,__popover__:!0,setup(e){const{mergedClsPrefixRef:t}=Ce(),r=re("Popconfirm","-popconfirm",Dl,Uo,e,t),n=I(null);function o(i){var a;if(!(!((a=n.value)===null||a===void 0)&&a.getMergedShow()))return;const{onPositiveClick:u,"onUpdate:show":d}=e;Promise.resolve(u?u(i):!0).then(g=>{var f;g!==!1&&((f=n.value)===null||f===void 0||f.setShow(!1),d&&fe(d,!1))})}function l(i){var a;if(!(!((a=n.value)===null||a===void 0)&&a.getMergedShow()))return;const{onNegativeClick:u,"onUpdate:show":d}=e;Promise.resolve(u?u(i):!0).then(g=>{var f;g!==!1&&((f=n.value)===null||f===void 0||f.setShow(!1),d&&fe(d,!1))})}return me(_n,{mergedThemeRef:r,mergedClsPrefixRef:t,props:e}),{setShow(i){var a;(a=n.value)===null||a===void 0||a.setShow(i)},syncPosition(){var i;(i=n.value)===null||i===void 0||i.syncPosition()},mergedTheme:r,popoverInstRef:n,handlePositiveClick:o,handleNegativeClick:l}},render(){const{$slots:e,$props:t,mergedTheme:r}=this;return p(xl,Zr(t,Fr,{theme:r.peers.Popover,themeOverrides:r.peerOverrides.Popover,internalExtraClass:["popconfirm"],ref:"popoverInstRef"}),{trigger:e.activator||e.trigger,default:()=>{const n=vn(t,Fr);return p(Hl,Object.assign(Object.assign({},n),{onPositiveClick:this.handlePositiveClick,onNegativeClick:this.handleNegativeClick}),e)}})}}),lr=Ae("n-tabs"),zn={tab:[String,Number,Object,Function],name:{type:[String,Number],required:!0},disabled:Boolean,displayDirective:{type:String,default:"if"},closable:{type:Boolean,default:void 0},tabProps:Object,label:[String,Number,Object,Function]},ht=U({__TAB_PANE__:!0,name:"TabPane",alias:["TabPanel"],props:zn,setup(e){const t=ae(lr,null);return t||or("tab-pane","`n-tab-pane` must be placed inside `n-tabs`."),{style:t.paneStyleRef,class:t.paneClassRef,mergedClsPrefix:t.mergedClsPrefixRef}},render(){return p("div",{class:[`${this.mergedClsPrefix}-tab-pane`,this.class],style:this.style},this.$slots)}}),Nl=Object.assign({internalLeftPadded:Boolean,internalAddable:Boolean,internalCreatedByPane:Boolean},Zr(zn,["displayDirective"])),Yt=U({__TAB__:!0,inheritAttrs:!1,name:"Tab",props:Nl,setup(e){const{mergedClsPrefixRef:t,valueRef:r,typeRef:n,closableRef:o,tabStyleRef:l,addTabStyleRef:s,tabClassRef:i,addTabClassRef:a,tabChangeIdRef:u,onBeforeLeaveRef:d,triggerRef:g,handleAdd:f,activateTab:c,handleClose:h}=ae(lr);return{trigger:g,mergedClosable:K(()=>{if(e.internalAddable)return!1;const{closable:b}=e;return b===void 0?o.value:b}),style:l,addStyle:s,tabClass:i,addTabClass:a,clsPrefix:t,value:r,type:n,handleClose(b){b.stopPropagation(),!e.disabled&&h(e.name)},activateTab(){if(e.disabled)return;if(e.internalAddable){f();return}const{name:b}=e,m=++u.id;if(b!==r.value){const{value:v}=d;v?Promise.resolve(v(e.name,r.value)).then(C=>{C&&u.id===m&&c(b)}):c(b)}}}},render(){const{internalAddable:e,clsPrefix:t,name:r,disabled:n,label:o,tab:l,value:s,mergedClosable:i,trigger:a,$slots:{default:u}}=this,d=o??l;return p("div",{class:`${t}-tabs-tab-wrapper`},this.internalLeftPadded?p("div",{class:`${t}-tabs-tab-pad`}):null,p("div",Object.assign({key:r,"data-name":r,"data-disabled":n?!0:void 0},tr({class:[`${t}-tabs-tab`,s===r&&`${t}-tabs-tab--active`,n&&`${t}-tabs-tab--disabled`,i&&`${t}-tabs-tab--closable`,e&&`${t}-tabs-tab--addable`,e?this.addTabClass:this.tabClass],onClick:a==="click"?this.activateTab:void 0,onMouseenter:a==="hover"?this.activateTab:void 0,style:e?this.addStyle:this.style},this.internalCreatedByPane?this.tabProps||{}:this.$attrs)),p("span",{class:`${t}-tabs-tab__label`},e?p(Be,null,p("div",{class:`${t}-tabs-tab__height-placeholder`}," "),p(nr,{clsPrefix:t},{default:()=>p(fl,null)})):u?u():typeof d=="object"?d:Vo(d??r)),i&&this.type==="card"?p(rr,{clsPrefix:t,class:`${t}-tabs-tab__close`,onClick:this.handleClose,disabled:n}):null))}}),Ul=y("tabs",`
 box-sizing: border-box;
 width: 100%;
 display: flex;
 flex-direction: column;
 transition:
 background-color .3s var(--n-bezier),
 border-color .3s var(--n-bezier);
`,[$("segment-type",[y("tabs-rail",[M("&.transition-disabled",[y("tabs-capsule",`
 transition: none;
 `)])])]),$("top",[y("tab-pane",`
 padding: var(--n-pane-padding-top) var(--n-pane-padding-right) var(--n-pane-padding-bottom) var(--n-pane-padding-left);
 `)]),$("left",[y("tab-pane",`
 padding: var(--n-pane-padding-right) var(--n-pane-padding-bottom) var(--n-pane-padding-left) var(--n-pane-padding-top);
 `)]),$("left, right",`
 flex-direction: row;
 `,[y("tabs-bar",`
 width: 2px;
 right: 0;
 transition:
 top .2s var(--n-bezier),
 max-height .2s var(--n-bezier),
 background-color .3s var(--n-bezier);
 `),y("tabs-tab",`
 padding: var(--n-tab-padding-vertical); 
 `)]),$("right",`
 flex-direction: row-reverse;
 `,[y("tab-pane",`
 padding: var(--n-pane-padding-left) var(--n-pane-padding-top) var(--n-pane-padding-right) var(--n-pane-padding-bottom);
 `),y("tabs-bar",`
 left: 0;
 `)]),$("bottom",`
 flex-direction: column-reverse;
 justify-content: flex-end;
 `,[y("tab-pane",`
 padding: var(--n-pane-padding-bottom) var(--n-pane-padding-right) var(--n-pane-padding-top) var(--n-pane-padding-left);
 `),y("tabs-bar",`
 top: 0;
 `)]),y("tabs-rail",`
 position: relative;
 padding: 3px;
 border-radius: var(--n-tab-border-radius);
 width: 100%;
 background-color: var(--n-color-segment);
 transition: background-color .3s var(--n-bezier);
 display: flex;
 align-items: center;
 `,[y("tabs-capsule",`
 border-radius: var(--n-tab-border-radius);
 position: absolute;
 pointer-events: none;
 background-color: var(--n-tab-color-segment);
 box-shadow: 0 1px 3px 0 rgba(0, 0, 0, .08);
 transition: transform 0.3s var(--n-bezier);
 `),y("tabs-tab-wrapper",`
 flex-basis: 0;
 flex-grow: 1;
 display: flex;
 align-items: center;
 justify-content: center;
 `,[y("tabs-tab",`
 overflow: hidden;
 border-radius: var(--n-tab-border-radius);
 width: 100%;
 display: flex;
 align-items: center;
 justify-content: center;
 `,[$("active",`
 font-weight: var(--n-font-weight-strong);
 color: var(--n-tab-text-color-active);
 `),M("&:hover",`
 color: var(--n-tab-text-color-hover);
 `)])])]),$("flex",[y("tabs-nav",`
 width: 100%;
 position: relative;
 `,[y("tabs-wrapper",`
 width: 100%;
 `,[y("tabs-tab",`
 margin-right: 0;
 `)])])]),y("tabs-nav",`
 box-sizing: border-box;
 line-height: 1.5;
 display: flex;
 transition: border-color .3s var(--n-bezier);
 `,[z("prefix, suffix",`
 display: flex;
 align-items: center;
 `),z("prefix","padding-right: 16px;"),z("suffix","padding-left: 16px;")]),$("top, bottom",[y("tabs-nav-scroll-wrapper",[M("&::before",`
 top: 0;
 bottom: 0;
 left: 0;
 width: 20px;
 `),M("&::after",`
 top: 0;
 bottom: 0;
 right: 0;
 width: 20px;
 `),$("shadow-start",[M("&::before",`
 box-shadow: inset 10px 0 8px -8px rgba(0, 0, 0, .12);
 `)]),$("shadow-end",[M("&::after",`
 box-shadow: inset -10px 0 8px -8px rgba(0, 0, 0, .12);
 `)])])]),$("left, right",[y("tabs-nav-scroll-content",`
 flex-direction: column;
 `),y("tabs-nav-scroll-wrapper",[M("&::before",`
 top: 0;
 left: 0;
 right: 0;
 height: 20px;
 `),M("&::after",`
 bottom: 0;
 left: 0;
 right: 0;
 height: 20px;
 `),$("shadow-start",[M("&::before",`
 box-shadow: inset 0 10px 8px -8px rgba(0, 0, 0, .12);
 `)]),$("shadow-end",[M("&::after",`
 box-shadow: inset 0 -10px 8px -8px rgba(0, 0, 0, .12);
 `)])])]),y("tabs-nav-scroll-wrapper",`
 flex: 1;
 position: relative;
 overflow: hidden;
 `,[y("tabs-nav-y-scroll",`
 height: 100%;
 width: 100%;
 overflow-y: auto; 
 scrollbar-width: none;
 `,[M("&::-webkit-scrollbar, &::-webkit-scrollbar-track-piece, &::-webkit-scrollbar-thumb",`
 width: 0;
 height: 0;
 display: none;
 `)]),M("&::before, &::after",`
 transition: box-shadow .3s var(--n-bezier);
 pointer-events: none;
 content: "";
 position: absolute;
 z-index: 1;
 `)]),y("tabs-nav-scroll-content",`
 display: flex;
 position: relative;
 min-width: 100%;
 min-height: 100%;
 width: fit-content;
 box-sizing: border-box;
 `),y("tabs-wrapper",`
 display: inline-flex;
 flex-wrap: nowrap;
 position: relative;
 `),y("tabs-tab-wrapper",`
 display: flex;
 flex-wrap: nowrap;
 flex-shrink: 0;
 flex-grow: 0;
 `),y("tabs-tab",`
 cursor: pointer;
 white-space: nowrap;
 flex-wrap: nowrap;
 display: inline-flex;
 align-items: center;
 color: var(--n-tab-text-color);
 font-size: var(--n-tab-font-size);
 background-clip: padding-box;
 padding: var(--n-tab-padding);
 transition:
 box-shadow .3s var(--n-bezier),
 color .3s var(--n-bezier),
 background-color .3s var(--n-bezier),
 border-color .3s var(--n-bezier);
 `,[$("disabled",{cursor:"not-allowed"}),z("close",`
 margin-left: 6px;
 transition:
 background-color .3s var(--n-bezier),
 color .3s var(--n-bezier);
 `),z("label",`
 display: flex;
 align-items: center;
 z-index: 1;
 `)]),y("tabs-bar",`
 position: absolute;
 bottom: 0;
 height: 2px;
 border-radius: 1px;
 background-color: var(--n-bar-color);
 transition:
 left .2s var(--n-bezier),
 max-width .2s var(--n-bezier),
 opacity .3s var(--n-bezier),
 background-color .3s var(--n-bezier);
 `,[M("&.transition-disabled",`
 transition: none;
 `),$("disabled",`
 background-color: var(--n-tab-text-color-disabled)
 `)]),y("tabs-pane-wrapper",`
 position: relative;
 overflow: hidden;
 transition: max-height .2s var(--n-bezier);
 `),y("tab-pane",`
 color: var(--n-pane-text-color);
 width: 100%;
 transition:
 color .3s var(--n-bezier),
 background-color .3s var(--n-bezier),
 opacity .2s var(--n-bezier);
 left: 0;
 right: 0;
 top: 0;
 `,[M("&.next-transition-leave-active, &.prev-transition-leave-active, &.next-transition-enter-active, &.prev-transition-enter-active",`
 transition:
 color .3s var(--n-bezier),
 background-color .3s var(--n-bezier),
 transform .2s var(--n-bezier),
 opacity .2s var(--n-bezier);
 `),M("&.next-transition-leave-active, &.prev-transition-leave-active",`
 position: absolute;
 `),M("&.next-transition-enter-from, &.prev-transition-leave-to",`
 transform: translateX(32px);
 opacity: 0;
 `),M("&.next-transition-leave-to, &.prev-transition-enter-from",`
 transform: translateX(-32px);
 opacity: 0;
 `),M("&.next-transition-leave-from, &.next-transition-enter-to, &.prev-transition-leave-from, &.prev-transition-enter-to",`
 transform: translateX(0);
 opacity: 1;
 `)]),y("tabs-tab-pad",`
 box-sizing: border-box;
 width: var(--n-tab-gap);
 flex-grow: 0;
 flex-shrink: 0;
 `),$("line-type, bar-type",[y("tabs-tab",`
 font-weight: var(--n-tab-font-weight);
 box-sizing: border-box;
 vertical-align: bottom;
 `,[M("&:hover",{color:"var(--n-tab-text-color-hover)"}),$("active",`
 color: var(--n-tab-text-color-active);
 font-weight: var(--n-tab-font-weight-active);
 `),$("disabled",{color:"var(--n-tab-text-color-disabled)"})])]),y("tabs-nav",[$("line-type",[$("top",[z("prefix, suffix",`
 border-bottom: 1px solid var(--n-tab-border-color);
 `),y("tabs-nav-scroll-content",`
 border-bottom: 1px solid var(--n-tab-border-color);
 `),y("tabs-bar",`
 bottom: -1px;
 `)]),$("left",[z("prefix, suffix",`
 border-right: 1px solid var(--n-tab-border-color);
 `),y("tabs-nav-scroll-content",`
 border-right: 1px solid var(--n-tab-border-color);
 `),y("tabs-bar",`
 right: -1px;
 `)]),$("right",[z("prefix, suffix",`
 border-left: 1px solid var(--n-tab-border-color);
 `),y("tabs-nav-scroll-content",`
 border-left: 1px solid var(--n-tab-border-color);
 `),y("tabs-bar",`
 left: -1px;
 `)]),$("bottom",[z("prefix, suffix",`
 border-top: 1px solid var(--n-tab-border-color);
 `),y("tabs-nav-scroll-content",`
 border-top: 1px solid var(--n-tab-border-color);
 `),y("tabs-bar",`
 top: -1px;
 `)]),z("prefix, suffix",`
 transition: border-color .3s var(--n-bezier);
 `),y("tabs-nav-scroll-content",`
 transition: border-color .3s var(--n-bezier);
 `),y("tabs-bar",`
 border-radius: 0;
 `)]),$("card-type",[z("prefix, suffix",`
 transition: border-color .3s var(--n-bezier);
 `),y("tabs-pad",`
 flex-grow: 1;
 transition: border-color .3s var(--n-bezier);
 `),y("tabs-tab-pad",`
 transition: border-color .3s var(--n-bezier);
 `),y("tabs-tab",`
 font-weight: var(--n-tab-font-weight);
 border: 1px solid var(--n-tab-border-color);
 background-color: var(--n-tab-color);
 box-sizing: border-box;
 position: relative;
 vertical-align: bottom;
 display: flex;
 justify-content: space-between;
 font-size: var(--n-tab-font-size);
 color: var(--n-tab-text-color);
 `,[$("addable",`
 padding-left: 8px;
 padding-right: 8px;
 font-size: 16px;
 justify-content: center;
 `,[z("height-placeholder",`
 width: 0;
 font-size: var(--n-tab-font-size);
 `),Te("disabled",[M("&:hover",`
 color: var(--n-tab-text-color-hover);
 `)])]),$("closable","padding-right: 8px;"),$("active",`
 background-color: #0000;
 font-weight: var(--n-tab-font-weight-active);
 color: var(--n-tab-text-color-active);
 `),$("disabled","color: var(--n-tab-text-color-disabled);")])]),$("left, right",`
 flex-direction: column; 
 `,[z("prefix, suffix",`
 padding: var(--n-tab-padding-vertical);
 `),y("tabs-wrapper",`
 flex-direction: column;
 `),y("tabs-tab-wrapper",`
 flex-direction: column;
 `,[y("tabs-tab-pad",`
 height: var(--n-tab-gap-vertical);
 width: 100%;
 `)])]),$("top",[$("card-type",[y("tabs-scroll-padding","border-bottom: 1px solid var(--n-tab-border-color);"),z("prefix, suffix",`
 border-bottom: 1px solid var(--n-tab-border-color);
 `),y("tabs-tab",`
 border-top-left-radius: var(--n-tab-border-radius);
 border-top-right-radius: var(--n-tab-border-radius);
 `,[$("active",`
 border-bottom: 1px solid #0000;
 `)]),y("tabs-tab-pad",`
 border-bottom: 1px solid var(--n-tab-border-color);
 `),y("tabs-pad",`
 border-bottom: 1px solid var(--n-tab-border-color);
 `)])]),$("left",[$("card-type",[y("tabs-scroll-padding","border-right: 1px solid var(--n-tab-border-color);"),z("prefix, suffix",`
 border-right: 1px solid var(--n-tab-border-color);
 `),y("tabs-tab",`
 border-top-left-radius: var(--n-tab-border-radius);
 border-bottom-left-radius: var(--n-tab-border-radius);
 `,[$("active",`
 border-right: 1px solid #0000;
 `)]),y("tabs-tab-pad",`
 border-right: 1px solid var(--n-tab-border-color);
 `),y("tabs-pad",`
 border-right: 1px solid var(--n-tab-border-color);
 `)])]),$("right",[$("card-type",[y("tabs-scroll-padding","border-left: 1px solid var(--n-tab-border-color);"),z("prefix, suffix",`
 border-left: 1px solid var(--n-tab-border-color);
 `),y("tabs-tab",`
 border-top-right-radius: var(--n-tab-border-radius);
 border-bottom-right-radius: var(--n-tab-border-radius);
 `,[$("active",`
 border-left: 1px solid #0000;
 `)]),y("tabs-tab-pad",`
 border-left: 1px solid var(--n-tab-border-color);
 `),y("tabs-pad",`
 border-left: 1px solid var(--n-tab-border-color);
 `)])]),$("bottom",[$("card-type",[y("tabs-scroll-padding","border-top: 1px solid var(--n-tab-border-color);"),z("prefix, suffix",`
 border-top: 1px solid var(--n-tab-border-color);
 `),y("tabs-tab",`
 border-bottom-left-radius: var(--n-tab-border-radius);
 border-bottom-right-radius: var(--n-tab-border-radius);
 `,[$("active",`
 border-top: 1px solid #0000;
 `)]),y("tabs-tab-pad",`
 border-top: 1px solid var(--n-tab-border-color);
 `),y("tabs-pad",`
 border-top: 1px solid var(--n-tab-border-color);
 `)])])])]),Vl=Object.assign(Object.assign({},re.props),{value:[String,Number],defaultValue:[String,Number],trigger:{type:String,default:"click"},type:{type:String,default:"bar"},closable:Boolean,justifyContent:String,size:{type:String,default:"medium"},placement:{type:String,default:"top"},tabStyle:[String,Object],tabClass:String,addTabStyle:[String,Object],addTabClass:String,barWidth:Number,paneClass:String,paneStyle:[String,Object],paneWrapperClass:String,paneWrapperStyle:[String,Object],addable:[Boolean,Object],tabsPadding:{type:Number,default:0},animated:Boolean,onBeforeLeave:Function,onAdd:Function,"onUpdate:value":[Function,Array],onUpdateValue:[Function,Array],onClose:[Function,Array],labelSize:String,activeName:[String,Number],onActiveNameChange:[Function,Array]}),Xl=U({name:"Tabs",props:Vl,setup(e,{slots:t}){var r,n,o,l;const{mergedClsPrefixRef:s,inlineThemeDisabled:i}=Ce(e),a=re("Tabs","-tabs",Ul,Xo,e,s),u=I(null),d=I(null),g=I(null),f=I(null),c=I(null),h=I(null),b=I(!0),m=I(!0),v=Nt(e,["labelSize","size"]),C=Nt(e,["activeName","value"]),R=I((n=(r=C.value)!==null&&r!==void 0?r:e.defaultValue)!==null&&n!==void 0?n:t.default?(l=(o=Me(t.default())[0])===null||o===void 0?void 0:o.props)===null||l===void 0?void 0:l.name:null),B=Kr(C,R),T={id:0},P=K(()=>{if(!(!e.justifyContent||e.type==="card"))return{display:"flex",justifyContent:e.justifyContent}});pe(B,()=>{T.id=0,Z(),N()});function S(){var x;const{value:w}=B;return w===null?null:(x=u.value)===null||x===void 0?void 0:x.querySelector(`[data-name="${w}"]`)}function D(x){if(e.type==="card")return;const{value:w}=d;if(!w)return;const k=w.style.opacity==="0";if(x){const L=`${s.value}-tabs-bar--disabled`,{barWidth:q,placement:ce}=e;if(x.dataset.disabled==="true"?w.classList.add(L):w.classList.remove(L),["top","bottom"].includes(ce)){if(O(["top","maxHeight","height"]),typeof q=="number"&&x.offsetWidth>=q){const ue=Math.floor((x.offsetWidth-q)/2)+x.offsetLeft;w.style.left=`${ue}px`,w.style.maxWidth=`${q}px`}else w.style.left=`${x.offsetLeft}px`,w.style.maxWidth=`${x.offsetWidth}px`;w.style.width="8192px",k&&(w.style.transition="none"),w.offsetWidth,k&&(w.style.transition="",w.style.opacity="1")}else{if(O(["left","maxWidth","width"]),typeof q=="number"&&x.offsetHeight>=q){const ue=Math.floor((x.offsetHeight-q)/2)+x.offsetTop;w.style.top=`${ue}px`,w.style.maxHeight=`${q}px`}else w.style.top=`${x.offsetTop}px`,w.style.maxHeight=`${x.offsetHeight}px`;w.style.height="8192px",k&&(w.style.transition="none"),w.offsetHeight,k&&(w.style.transition="",w.style.opacity="1")}}}function j(){if(e.type==="card")return;const{value:x}=d;x&&(x.style.opacity="0")}function O(x){const{value:w}=d;if(w)for(const k of x)w.style[k]=""}function Z(){if(e.type==="card")return;const x=S();x?D(x):j()}function N(){var x;const w=(x=c.value)===null||x===void 0?void 0:x.$el;if(!w)return;const k=S();if(!k)return;const{scrollLeft:L,offsetWidth:q}=w,{offsetLeft:ce,offsetWidth:ue}=k;L>ce?w.scrollTo({top:0,left:ce,behavior:"smooth"}):ce+ue>L+q&&w.scrollTo({top:0,left:ce+ue-q,behavior:"smooth"})}const X=I(null);let ne=0,_=null;function V(x){const w=X.value;if(w){ne=x.getBoundingClientRect().height;const k=`${ne}px`,L=()=>{w.style.height=k,w.style.maxHeight=k};_?(L(),_(),_=null):_=L}}function ee(x){const w=X.value;if(w){const k=x.getBoundingClientRect().height,L=()=>{document.body.offsetHeight,w.style.maxHeight=`${k}px`,w.style.height=`${Math.max(ne,k)}px`};_?(_(),_=null,L()):_=L}}function he(){const x=X.value;if(x){x.style.maxHeight="",x.style.height="";const{paneWrapperStyle:w}=e;if(typeof w=="string")x.style.cssText=w;else if(w){const{maxHeight:k,height:L}=w;k!==void 0&&(x.style.maxHeight=k),L!==void 0&&(x.style.height=L)}}}const de={value:[]},oe=I("next");function Ze(x){const w=B.value;let k="next";for(const L of de.value){if(L===w)break;if(L===x){k="prev";break}}oe.value=k,lt(x)}function lt(x){const{onActiveNameChange:w,onUpdateValue:k,"onUpdate:value":L}=e;w&&fe(w,x),k&&fe(k,x),L&&fe(L,x),R.value=x}function Ct(x){const{onClose:w}=e;w&&fe(w,x)}function st(){const{value:x}=d;if(!x)return;const w="transition-disabled";x.classList.add(w),Z(),x.classList.remove(w)}const $e=I(null);function Le({transitionDisabled:x}){const w=u.value;if(!w)return;x&&w.classList.add("transition-disabled");const k=S();k&&$e.value&&($e.value.style.width=`${k.offsetWidth}px`,$e.value.style.height=`${k.offsetHeight}px`,$e.value.style.transform=`translateX(${k.offsetLeft-jt(getComputedStyle(w).paddingLeft)}px)`,x&&$e.value.offsetWidth),x&&w.classList.remove("transition-disabled")}pe([B],()=>{e.type==="segment"&&pt(()=>{Le({transitionDisabled:!1})})}),we(()=>{e.type==="segment"&&Le({transitionDisabled:!0})});let dt=0;function $t(x){var w;if(x.contentRect.width===0&&x.contentRect.height===0||dt===x.contentRect.width)return;dt=x.contentRect.width;const{type:k}=e;if((k==="line"||k==="bar")&&st(),k!=="segment"){const{placement:L}=e;_t((L==="top"||L==="bottom"?(w=c.value)===null||w===void 0?void 0:w.$el:h.value)||null)}}const St=Ot($t,64);pe([()=>e.justifyContent,()=>e.size],()=>{pt(()=>{const{type:x}=e;(x==="line"||x==="bar")&&st()})});const Se=I(!1);function Re(x){var w;const{target:k,contentRect:{width:L,height:q}}=x,ce=k.parentElement.parentElement.offsetWidth,ue=k.parentElement.parentElement.offsetHeight,{placement:We}=e;if(!Se.value)We==="top"||We==="bottom"?ce<L&&(Se.value=!0):ue<q&&(Se.value=!0);else{const{value:Je}=f;if(!Je)return;We==="top"||We==="bottom"?ce-L>Je.$el.offsetWidth&&(Se.value=!1):ue-q>Je.$el.offsetHeight&&(Se.value=!1)}_t(((w=c.value)===null||w===void 0?void 0:w.$el)||null)}const kn=Ot(Re,64);function In(){const{onAdd:x}=e;x&&x(),pt(()=>{const w=S(),{value:k}=c;!w||!k||k.scrollTo({left:w.offsetLeft,top:0,behavior:"smooth"})})}function _t(x){if(!x)return;const{placement:w}=e;if(w==="top"||w==="bottom"){const{scrollLeft:k,scrollWidth:L,offsetWidth:q}=x;b.value=k<=0,m.value=k+q>=L}else{const{scrollTop:k,scrollHeight:L,offsetHeight:q}=x;b.value=k<=0,m.value=k+q>=L}}const En=Ot(x=>{_t(x.target)},64);me(lr,{triggerRef:G(e,"trigger"),tabStyleRef:G(e,"tabStyle"),tabClassRef:G(e,"tabClass"),addTabStyleRef:G(e,"addTabStyle"),addTabClassRef:G(e,"addTabClass"),paneClassRef:G(e,"paneClass"),paneStyleRef:G(e,"paneStyle"),mergedClsPrefixRef:s,typeRef:G(e,"type"),closableRef:G(e,"closable"),valueRef:B,tabChangeIdRef:T,onBeforeLeaveRef:G(e,"onBeforeLeave"),activateTab:Ze,handleClose:Ct,handleAdd:In}),tn(()=>{Z(),N()}),er(()=>{const{value:x}=g;if(!x)return;const{value:w}=s,k=`${w}-tabs-nav-scroll-wrapper--shadow-start`,L=`${w}-tabs-nav-scroll-wrapper--shadow-end`;b.value?x.classList.remove(k):x.classList.add(k),m.value?x.classList.remove(L):x.classList.add(L)});const An={syncBarPosition:()=>{Z()}},Bn=()=>{Le({transitionDisabled:!0})},sr=K(()=>{const{value:x}=v,{type:w}=e,k={card:"Card",bar:"Bar",line:"Line",segment:"Segment"}[w],L=`${x}${k}`,{self:{barColor:q,closeIconColor:ce,closeIconColorHover:ue,closeIconColorPressed:We,tabColor:Je,tabBorderColor:Mn,paneTextColor:On,tabFontWeight:Ln,tabBorderRadius:Rn,tabFontWeightActive:Fn,colorSegment:Wn,fontWeightStrong:Hn,tabColorSegment:Dn,closeSize:jn,closeIconSize:Nn,closeColorHover:Un,closeColorPressed:Vn,closeBorderRadius:Xn,[W("panePadding",x)]:ct,[W("tabPadding",L)]:Gn,[W("tabPaddingVertical",L)]:Kn,[W("tabGap",L)]:qn,[W("tabGap",`${L}Vertical`)]:Yn,[W("tabTextColor",w)]:Zn,[W("tabTextColorActive",w)]:Jn,[W("tabTextColorHover",w)]:Qn,[W("tabTextColorDisabled",w)]:eo,[W("tabFontSize",x)]:to},common:{cubicBezierEaseInOut:ro}}=a.value;return{"--n-bezier":ro,"--n-color-segment":Wn,"--n-bar-color":q,"--n-tab-font-size":to,"--n-tab-text-color":Zn,"--n-tab-text-color-active":Jn,"--n-tab-text-color-disabled":eo,"--n-tab-text-color-hover":Qn,"--n-pane-text-color":On,"--n-tab-border-color":Mn,"--n-tab-border-radius":Rn,"--n-close-size":jn,"--n-close-icon-size":Nn,"--n-close-color-hover":Un,"--n-close-color-pressed":Vn,"--n-close-border-radius":Xn,"--n-close-icon-color":ce,"--n-close-icon-color-hover":ue,"--n-close-icon-color-pressed":We,"--n-tab-color":Je,"--n-tab-font-weight":Ln,"--n-tab-font-weight-active":Fn,"--n-tab-padding":Gn,"--n-tab-padding-vertical":Kn,"--n-tab-gap":qn,"--n-tab-gap-vertical":Yn,"--n-pane-padding-left":Ne(ct,"left"),"--n-pane-padding-right":Ne(ct,"right"),"--n-pane-padding-top":Ne(ct,"top"),"--n-pane-padding-bottom":Ne(ct,"bottom"),"--n-font-weight-strong":Hn,"--n-tab-color-segment":Dn}}),Fe=i?Oe("tabs",K(()=>`${v.value[0]}${e.type[0]}`),sr,e):void 0;return Object.assign({mergedClsPrefix:s,mergedValue:B,renderedNames:new Set,segmentCapsuleElRef:$e,tabsPaneWrapperRef:X,tabsElRef:u,barElRef:d,addTabInstRef:f,xScrollInstRef:c,scrollWrapperElRef:g,addTabFixed:Se,tabWrapperStyle:P,handleNavResize:St,mergedSize:v,handleScroll:En,handleTabsResize:kn,cssVars:i?void 0:sr,themeClass:Fe==null?void 0:Fe.themeClass,animationDirection:oe,renderNameListRef:de,yScrollElRef:h,handleSegmentResize:Bn,onAnimationBeforeLeave:V,onAnimationEnter:ee,onAnimationAfterEnter:he,onRender:Fe==null?void 0:Fe.onRender},An)},render(){const{mergedClsPrefix:e,type:t,placement:r,addTabFixed:n,addable:o,mergedSize:l,renderNameListRef:s,onRender:i,paneWrapperClass:a,paneWrapperStyle:u,$slots:{default:d,prefix:g,suffix:f}}=this;i==null||i();const c=d?Me(d()).filter(T=>T.type.__TAB_PANE__===!0):[],h=d?Me(d()).filter(T=>T.type.__TAB__===!0):[],b=!h.length,m=t==="card",v=t==="segment",C=!m&&!v&&this.justifyContent;s.value=[];const R=()=>{const T=p("div",{style:this.tabWrapperStyle,class:`${e}-tabs-wrapper`},C?null:p("div",{class:`${e}-tabs-scroll-padding`,style:r==="top"||r==="bottom"?{width:`${this.tabsPadding}px`}:{height:`${this.tabsPadding}px`}}),b?c.map((P,S)=>(s.value.push(P.props.name),Ft(p(Yt,Object.assign({},P.props,{internalCreatedByPane:!0,internalLeftPadded:S!==0&&(!C||C==="center"||C==="start"||C==="end")}),P.children?{default:P.children.tab}:void 0)))):h.map((P,S)=>(s.value.push(P.props.name),Ft(S!==0&&!C?Dr(P):P))),!n&&o&&m?Hr(o,(b?c.length:h.length)!==0):null,C?null:p("div",{class:`${e}-tabs-scroll-padding`,style:{width:`${this.tabsPadding}px`}}));return p("div",{ref:"tabsElRef",class:`${e}-tabs-nav-scroll-content`},m&&o?p(zt,{onResize:this.handleTabsResize},{default:()=>T}):T,m?p("div",{class:`${e}-tabs-pad`}):null,m?null:p("div",{ref:"barElRef",class:`${e}-tabs-bar`}))},B=v?"top":r;return p("div",{class:[`${e}-tabs`,this.themeClass,`${e}-tabs--${t}-type`,`${e}-tabs--${l}-size`,C&&`${e}-tabs--flex`,`${e}-tabs--${B}`],style:this.cssVars},p("div",{class:[`${e}-tabs-nav--${t}-type`,`${e}-tabs-nav--${B}`,`${e}-tabs-nav`]},ye(g,T=>T&&p("div",{class:`${e}-tabs-nav__prefix`},T)),v?p(zt,{onResize:this.handleSegmentResize},{default:()=>p("div",{class:`${e}-tabs-rail`,ref:"tabsElRef"},p("div",{class:`${e}-tabs-capsule`,ref:"segmentCapsuleElRef"},p("div",{class:`${e}-tabs-wrapper`},p("div",{class:`${e}-tabs-tab`}))),b?c.map((T,P)=>(s.value.push(T.props.name),p(Yt,Object.assign({},T.props,{internalCreatedByPane:!0,internalLeftPadded:P!==0}),T.children?{default:T.children.tab}:void 0))):h.map((T,P)=>(s.value.push(T.props.name),P===0?T:Dr(T))))}):p(zt,{onResize:this.handleNavResize},{default:()=>p("div",{class:`${e}-tabs-nav-scroll-wrapper`,ref:"scrollWrapperElRef"},["top","bottom"].includes(B)?p(Oa,{ref:"xScrollInstRef",onScroll:this.handleScroll},{default:R}):p("div",{class:`${e}-tabs-nav-y-scroll`,onScroll:this.handleScroll,ref:"yScrollElRef"},R()))}),n&&o&&m?Hr(o,!0):null,ye(f,T=>T&&p("div",{class:`${e}-tabs-nav__suffix`},T))),b&&(this.animated&&(B==="top"||B==="bottom")?p("div",{ref:"tabsPaneWrapperRef",style:u,class:[`${e}-tabs-pane-wrapper`,a]},Wr(c,this.mergedValue,this.renderedNames,this.onAnimationBeforeLeave,this.onAnimationEnter,this.onAnimationAfterEnter,this.animationDirection)):Wr(c,this.mergedValue,this.renderedNames)))}});function Wr(e,t,r,n,o,l,s){const i=[];return e.forEach(a=>{const{name:u,displayDirective:d,"display-directive":g}=a.props,f=h=>d===h||g===h,c=t===u;if(a.key!==void 0&&(a.key=u),c||f("show")||f("show:lazy")&&r.has(u)){r.has(u)||r.add(u);const h=!f("if");i.push(h?ot(a,[[Xr,c]]):a)}}),s?p(Go,{name:`${s}-transition`,onBeforeLeave:n,onEnter:o,onAfterEnter:l},{default:()=>i}):i}function Hr(e,t){return p(Yt,{ref:"addTabInstRef",key:"__addable",name:"__addable",internalCreatedByPane:!0,internalAddable:!0,internalLeftPadded:t,disabled:typeof e=="object"&&e.disabled})}function Dr(e){const t=Gr(e);return t.props?t.props.internalLeftPadded=!0:t.props={internalLeftPadded:!0},t}function Ft(e){return Array.isArray(e.dynamicProps)?e.dynamicProps.includes("internalLeftPadded")||e.dynamicProps.push("internalLeftPadded"):e.dynamicProps=["internalLeftPadded"],e}const Gl=y("thing",`
 display: flex;
 transition: color .3s var(--n-bezier);
 font-size: var(--n-font-size);
 color: var(--n-text-color);
`,[y("thing-avatar",`
 margin-right: 12px;
 margin-top: 2px;
 `),y("thing-avatar-header-wrapper",`
 display: flex;
 flex-wrap: nowrap;
 `,[y("thing-header-wrapper",`
 flex: 1;
 `)]),y("thing-main",`
 flex-grow: 1;
 `,[y("thing-header",`
 display: flex;
 margin-bottom: 4px;
 justify-content: space-between;
 align-items: center;
 `,[z("title",`
 font-size: 16px;
 font-weight: var(--n-title-font-weight);
 transition: color .3s var(--n-bezier);
 color: var(--n-title-text-color);
 `)]),z("description",[M("&:not(:last-child)",`
 margin-bottom: 4px;
 `)]),z("content",[M("&:not(:first-child)",`
 margin-top: 12px;
 `)]),z("footer",[M("&:not(:first-child)",`
 margin-top: 12px;
 `)]),z("action",[M("&:not(:first-child)",`
 margin-top: 12px;
 `)])])]),Kl=Object.assign(Object.assign({},re.props),{title:String,titleExtra:String,description:String,descriptionClass:String,descriptionStyle:[String,Object],content:String,contentClass:String,contentStyle:[String,Object],contentIndented:Boolean}),Tn=U({name:"Thing",props:Kl,setup(e,{slots:t}){const{mergedClsPrefixRef:r,inlineThemeDisabled:n,mergedRtlRef:o}=Ce(e),l=re("Thing","-thing",Gl,Ko,e,r),s=it("Thing",o,r),i=K(()=>{const{self:{titleTextColor:u,textColor:d,titleFontWeight:g,fontSize:f},common:{cubicBezierEaseInOut:c}}=l.value;return{"--n-bezier":c,"--n-font-size":f,"--n-text-color":d,"--n-title-font-weight":g,"--n-title-text-color":u}}),a=n?Oe("thing",void 0,i,e):void 0;return()=>{var u;const{value:d}=r,g=s?s.value:!1;return(u=a==null?void 0:a.onRender)===null||u===void 0||u.call(a),p("div",{class:[`${d}-thing`,a==null?void 0:a.themeClass,g&&`${d}-thing--rtl`],style:n?void 0:i.value},t.avatar&&e.contentIndented?p("div",{class:`${d}-thing-avatar`},t.avatar()):null,p("div",{class:`${d}-thing-main`},!e.contentIndented&&(t.header||e.title||t["header-extra"]||e.titleExtra||t.avatar)?p("div",{class:`${d}-thing-avatar-header-wrapper`},t.avatar?p("div",{class:`${d}-thing-avatar`},t.avatar()):null,t.header||e.title||t["header-extra"]||e.titleExtra?p("div",{class:`${d}-thing-header-wrapper`},p("div",{class:`${d}-thing-header`},t.header||e.title?p("div",{class:`${d}-thing-header__title`},t.header?t.header():e.title):null,t["header-extra"]||e.titleExtra?p("div",{class:`${d}-thing-header__extra`},t["header-extra"]?t["header-extra"]():e.titleExtra):null),t.description||e.description?p("div",{class:[`${d}-thing-main__description`,e.descriptionClass],style:e.descriptionStyle},t.description?t.description():e.description):null):null):p(Be,null,t.header||e.title||t["header-extra"]||e.titleExtra?p("div",{class:`${d}-thing-header`},t.header||e.title?p("div",{class:`${d}-thing-header__title`},t.header?t.header():e.title):null,t["header-extra"]||e.titleExtra?p("div",{class:`${d}-thing-header__extra`},t["header-extra"]?t["header-extra"]():e.titleExtra):null):null,t.description||e.description?p("div",{class:[`${d}-thing-main__description`,e.descriptionClass],style:e.descriptionStyle},t.description?t.description():e.description):null),t.default||e.content?p("div",{class:[`${d}-thing-main__content`,e.contentClass],style:e.contentStyle},t.default?t.default():e.content):null,t.footer?p("div",{class:`${d}-thing-main__footer`},t.footer()):null,t.action?p("div",{class:`${d}-thing-main__action`},t.action()):null))}}}),ql={class:"topbar"},Yl={class:"brand-block"},Zl={class:"version"},Jl={class:"topnav","aria-label":"Primary"},Ql=["aria-current"],es=["aria-current"],ts=["aria-current"],rs=U({__name:"Topbar",props:{active:{}},setup(e){const t=I(null),r=I("version dev");we(async()=>{try{t.value=await qo()}catch{}try{r.value=await Yo()}catch{}});async function n(){try{await Zo()}catch{}finally{location.assign("/login.html")}}return(o,l)=>{var s;return Y(),Ee("header",ql,[te("div",Yl,[l[0]||(l[0]=te("div",{class:"brand"},"AT Term",-1)),te("div",Zl,ve(r.value),1)]),te("nav",Jl,[te("a",{href:"/",class:Tt({active:o.active==="home"}),"aria-current":o.active==="home"?"page":!1},"Home",10,Ql),te("a",{href:"/settings.html",class:Tt({active:o.active==="settings"}),"aria-current":o.active==="settings"?"page":!1},"Settings",10,es),(s=t.value)!=null&&s.is_admin?(Y(),Ee("a",{key:0,href:"/admin/",class:Tt({active:o.active==="admin"}),"aria-current":o.active==="admin"?"page":!1},"Admin",10,ts)):ke("",!0)]),te("button",{type:"button",class:"ghost-btn",onClick:n},"Sign out")])}}}),ns=Ye(rs,[["__scopeId","data-v-232a58ee"]]),os={class:"plaintext-display"},as={key:2,class:"empty"},is=U({__name:"ApiTokens",setup(e){const t=I([]),r=I(""),n=I(!1),o=I(""),l=I(!0),s=wn();function i(c){try{return new Date(c).toLocaleDateString()}catch{return c}}function a(c){return!c.revoked_at}async function u(){l.value=!0;try{t.value=await Jo()}catch(c){c instanceof Ie&&s.error("Failed to load tokens.")}finally{l.value=!1}}async function d(c){c.preventDefault();const h=r.value.trim();if(!(!h||n.value)){n.value=!0;try{const b=await Qo(h);o.value=b.plaintext,r.value="",await u()}catch(b){b instanceof Ie&&(b.code==="name_required"?s.error("Token name is required."):b.code==="invalid_request"?s.error("Please enter a valid name."):s.error("Failed to create token."))}finally{n.value=!1}}}async function g(c){try{await ea(c),await u()}catch(h){h instanceof Ie&&s.error("Failed to revoke token.")}}async function f(){try{await navigator.clipboard.writeText(o.value),s.success("Token copied to clipboard.")}catch{s.warning("Clipboard not available — select and copy manually.")}}return we(u),(c,h)=>(Y(),le(A(wt),{title:"API Tokens",bordered:!1},{default:E(()=>[o.value?(Y(),le(A(Al),{key:0,type:"success","show-icon":!1,class:"plaintext-alert"},{default:E(()=>[h[2]||(h[2]=te("div",{class:"plaintext-msg"},"Copy this token now — it will not be shown again.",-1)),te("code",os,ve(o.value),1),F(A(xe),{size:"small",tertiary:"",class:"plaintext-copy",onClick:f},{default:E(()=>h[1]||(h[1]=[Q("Copy")])),_:1})]),_:1})):ke("",!0),t.value.filter(a).length>0?(Y(),le(A($n),{key:1,bordered:""},{default:E(()=>[(Y(!0),Ee(Be,null,Jr(t.value.filter(a),b=>(Y(),le(A(Sn),{key:b.id},{suffix:E(()=>[F(A(yt),{onPositiveClick:m=>g(b.id)},{trigger:E(()=>[F(A(xe),{size:"small",type:"error","data-testid":`revoke-${b.id}`},{default:E(()=>h[3]||(h[3]=[Q(" Revoke ")])),_:2},1032,["data-testid"])]),default:E(()=>[h[4]||(h[4]=Q(" Revoke this token? This cannot be undone. "))]),_:2},1032,["onPositiveClick"])]),default:E(()=>[F(A(Tn),null,{header:E(()=>[Q(ve(b.name),1)]),description:E(()=>[te("code",null,ve(b.prefix)+"…",1),Q(" · created "+ve(i(b.created_at)),1)]),_:2},1024)]),_:2},1024))),128))]),_:1})):l.value?ke("",!0):(Y(),Ee("p",as,"No tokens yet.")),te("form",{class:"create-form",onSubmit:d,autocomplete:"off"},[F(A(Rl),{wrap:!1},{default:E(()=>[F(A(nt),{value:r.value,"onUpdate:value":h[0]||(h[0]=b=>r.value=b),type:"text",placeholder:"e.g. my-laptop","input-props":{required:!0,autocomplete:"off"}},null,8,["value"]),F(A(xe),{type:"primary","attr-type":"submit",loading:n.value,disabled:n.value},{default:E(()=>h[5]||(h[5]=[Q(" Create ")])),_:1},8,["loading","disabled"])]),_:1})],32)]),_:1}))}}),ls=Ye(is,[["__scopeId","data-v-07870487"]]),ss={key:0,class:"form-error",role:"alert"},ds=U({__name:"ChangePassword",setup(e){const t=I(""),r=I(""),n=I(!1),o=I("");function l(i){if(i instanceof Ie){if(i.code==="current_password_wrong")return"Current password is incorrect.";if(i.code==="password_weak")return"New password must be at least 12 characters.";if(i.code==="invalid_request")return"Please check your input."}return"Password change failed. Please try again."}async function s(i){if(i.preventDefault(),!n.value){o.value="",n.value=!0;try{await ta(t.value,r.value),location.assign("/login.html")}catch(a){o.value=l(a)}finally{n.value=!1}}}return(i,a)=>(Y(),le(A(wt),{title:"Change Password",bordered:!1},{default:E(()=>[te("form",{onSubmit:s,autocomplete:"off",novalidate:""},[F(A(Qr),{"label-placement":"top","require-mark-placement":"right-hanging"},{default:E(()=>[F(A(bt),{label:"Current password","show-feedback":!1},{default:E(()=>[F(A(nt),{value:t.value,"onUpdate:value":a[0]||(a[0]=u=>t.value=u),type:"password","show-password-on":"click","input-props":{required:!0,autocomplete:"current-password"}},null,8,["value"])]),_:1}),F(A(bt),{label:"New password (min 12 characters)","show-feedback":!1},{default:E(()=>[F(A(nt),{value:r.value,"onUpdate:value":a[1]||(a[1]=u=>r.value=u),type:"password","show-password-on":"click","input-props":{required:!0,autocomplete:"new-password",minlength:12}},null,8,["value"])]),_:1}),F(A(xe),{type:"primary","attr-type":"submit",loading:n.value,disabled:n.value},{default:E(()=>a[2]||(a[2]=[Q(" Update password ")])),_:1},8,["loading","disabled"]),o.value?(Y(),Ee("p",ss,ve(o.value),1)):ke("",!0)]),_:1})],32)]),_:1}))}}),cs=Ye(ds,[["__scopeId","data-v-33f39658"]]),us={key:1,class:"empty"},fs={class:"actions"},hs=U({__name:"Sessions",setup(e){const t=I([]),r=I(!0),n=I(!1),o=wn();function l(d){return d?d.includes("Firefox")?"Firefox":d.includes("Edg/")?"Edge":d.includes("Chrome")?"Chrome":d.includes("Safari")?"Safari":d.length>40?d.slice(0,40)+"…":d:"Unknown device"}function s(d){try{return new Date(d).toLocaleString()}catch{return""}}async function i(){r.value=!0;try{t.value=await ra()}catch(d){d instanceof Ie&&o.error("Failed to load sessions.")}finally{r.value=!1}}async function a(d){try{await na(d),await i()}catch(g){g instanceof Ie&&o.error("Revoke failed.")}}async function u(){if(!n.value){n.value=!0;try{const d=await oa();o.success(`Signed out ${d.deleted} other device${d.deleted===1?"":"s"}.`),await i()}catch(d){d instanceof Ie&&o.error("Sign-out-others failed.")}finally{n.value=!1}}}return we(i),(d,g)=>(Y(),le(A(wt),{title:"Signed-in devices",bordered:!1},{default:E(()=>[g[5]||(g[5]=te("p",{class:"muted"},"Each row is a browser or PWA where this account is signed in.",-1)),t.value.length>0?(Y(),le(A($n),{key:0,bordered:""},{default:E(()=>[(Y(!0),Ee(Be,null,Jr(t.value,f=>(Y(),le(A(Sn),{key:f.id_hash},{suffix:E(()=>[f.is_current?ke("",!0):(Y(),le(A(yt),{key:0,onPositiveClick:c=>a(f.id_hash)},{trigger:E(()=>[F(A(xe),{size:"small",type:"error","data-testid":`revoke-session-${f.id_hash}`},{default:E(()=>g[1]||(g[1]=[Q(" Revoke ")])),_:2},1032,["data-testid"])]),default:E(()=>[g[2]||(g[2]=Q(" Revoke this device? You'll need to sign in again on it. "))]),_:2},1032,["onPositiveClick"]))]),default:E(()=>[F(A(Tn),null,{header:E(()=>[Q(ve(l(f.user_agent))+" ",1),f.is_current?(Y(),le(A(zl),{key:0,type:"success",size:"small",round:"",style:{"margin-left":"0.5rem"}},{default:E(()=>g[0]||(g[0]=[Q(" this device ")])),_:1})):ke("",!0)]),description:E(()=>[Q(" signed in "+ve(s(f.created_at))+" · "+ve(f.ip_prefix||"ip unknown"),1)]),_:2},1024)]),_:2},1024))),128))]),_:1})):r.value?ke("",!0):(Y(),Ee("p",us,"No active sessions.")),te("div",fs,[F(A(yt),{onPositiveClick:u},{trigger:E(()=>[F(A(xe),{type:"error",loading:n.value,disabled:n.value,"data-testid":"sign-out-others"},{default:E(()=>g[3]||(g[3]=[Q(" Sign out everywhere except this device ")])),_:1},8,["loading","disabled"])]),default:E(()=>[g[4]||(g[4]=Q(" Sign out every other device? They'll all need to sign in again. "))]),_:1})])]),_:1}))}}),ps=Ye(hs,[["__scopeId","data-v-52a2a654"]]),vs={key:0,class:"form-error",role:"alert"},bs=U({__name:"DangerZone",setup(e){const t=I(""),r=I(""),n=I(!1),o=I("");function l(a){if(a instanceof Ie){if(a.code==="email_mismatch")return"Email doesn't match — type your exact email.";if(a.code==="password_incorrect")return"Password is incorrect.";if(a.code==="last_admin")return"You're the last admin — promote another user first.";if(a.code==="invalid_request")return"Please check your input."}return"Delete failed. Please try again."}async function s(){if(!n.value){o.value="",n.value=!0;try{await aa(t.value.trim(),r.value),location.assign("/login.html")}catch(a){o.value=l(a)}finally{n.value=!1}}}function i(a){a.preventDefault()}return(a,u)=>(Y(),le(A(wt),{title:"Danger zone",bordered:!1,class:"danger-card"},{default:E(()=>[u[4]||(u[4]=te("p",null,` Permanently delete this account. This cannot be undone. API tokens, web sessions, and account data are removed. Invitations you've consumed stay (their "consumed by" field is cleared). `,-1)),te("form",{onSubmit:i,autocomplete:"off",novalidate:""},[F(A(Qr),{"label-placement":"top","require-mark-placement":"right-hanging"},{default:E(()=>[F(A(bt),{label:"Confirm by typing your full email","show-feedback":!1},{default:E(()=>[F(A(nt),{value:t.value,"onUpdate:value":u[0]||(u[0]=d=>t.value=d),type:"text","input-props":{type:"email",required:!0,autocomplete:"off"}},null,8,["value"])]),_:1}),F(A(bt),{label:"Current password","show-feedback":!1},{default:E(()=>[F(A(nt),{value:r.value,"onUpdate:value":u[1]||(u[1]=d=>r.value=d),type:"password","show-password-on":"click","input-props":{required:!0,autocomplete:"current-password"}},null,8,["value"])]),_:1}),F(A(yt),{onPositiveClick:s},{trigger:E(()=>[F(A(xe),{type:"error","attr-type":"button",loading:n.value,disabled:n.value,"data-testid":"delete-account-trigger"},{default:E(()=>u[2]||(u[2]=[Q(" Delete my account ")])),_:1},8,["loading","disabled"])]),default:E(()=>[u[3]||(u[3]=Q(" Permanently delete this account? This cannot be undone. "))]),_:1}),o.value?(Y(),Ee("p",vs,ve(o.value),1)):ke("",!0)]),_:1})],32)]),_:1}))}}),gs=Ye(bs,[["__scopeId","data-v-f9c6c4b3"]]),ms={class:"settings-page"},ys=U({__name:"App",setup(e){const t=["api-tokens","change-password","sessions","danger"];function r(){const i=location.hash.replace(/^#/,"");return t.includes(i)?i:"api-tokens"}const n=I(r());function o(){n.value=r()}we(()=>window.addEventListener("hashchange",o)),ia(()=>window.removeEventListener("hashchange",o));function l(i){t.includes(i)&&location.hash.replace(/^#/,"")!==i&&(location.hash="#"+i)}const s=la();return(i,a)=>(Y(),le(A(ca),{theme:A(da),"theme-overrides":A(s)},{default:E(()=>[F(A(sa),null,{default:E(()=>[F(ns,{active:"settings"}),te("main",ms,[F(A(Xl),{value:n.value,type:"line",animated:"","onUpdate:value":l},{default:E(()=>[F(A(ht),{name:"api-tokens",tab:"API Tokens"},{default:E(()=>[F(ls)]),_:1}),F(A(ht),{name:"change-password",tab:"Change Password"},{default:E(()=>[F(cs)]),_:1}),F(A(ht),{name:"sessions",tab:"Signed-in devices"},{default:E(()=>[F(ps)]),_:1}),F(A(ht),{name:"danger",tab:"Danger zone"},{default:E(()=>[F(gs)]),_:1})]),_:1},8,["value"])])]),_:1})]),_:1},8,["theme","theme-overrides"]))}}),xs=Ye(ys,[["__scopeId","data-v-223e476e"]]);ua(xs).mount("#app");
