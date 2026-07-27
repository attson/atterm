import{a1 as ge,aS as se,aQ as ee,bl as De,a as Ar,bp as $n,aE as Tn,a0 as Or,an as Ct,bt as Rt,aK as Lr,aF as Ne,am as rt,b6 as je,aT as Fr,aH as Wr,ay as Bt,t as Rr,ax as Ie,x as Pn,bf as Me,M as St,b as Hr,p as Ht,ae as Dr,U as Dt,aA as Nt,n as Je,aL as Nr,aG as jt,K as jr,be as Mt,aD as Ur,aB as Vr,aw as Xr,aC as zn,ah as Kr,w as Yr,aq as Gr,v as Zr,u as qr,z as I,A as m,H as pe,F as O,G as $,D as Jr,bh as Ue,br as de,aW as Qr,ag as ct,bs as ot,aJ as Ut,b4 as be,r as eo,bn as _n,J as oe,a9 as to,R as no,L as W,N as En,bo as ro,a2 as H,ak as Re,P as Vt,aO as oo,bb as kn,b2 as Xt,B as Kt,c as In,W as ao,bk as Yt,aM as io,aR as Bn,aV as so,a_ as lo,V as ut,b9 as co,a8 as uo,bj as fo}from"./mobile-guard-COp2-_z5.js";import{Y as M,X as Gt,ai as ie,s as Mn,Q as Ae,O as Oe,g as K,x as Q,n as ho,F as at,C as po,p as G,V as ve,al as Ve,u as y,T as bo,a9 as D,J as Qe,b as vo,aj as At,ag as An,I as On,f as Ln,a as go,c as mo,U as Zt,l as qt,i as me,a7 as $e,ab as Te,K as ft,k as yo,d as xo}from"./relay-config-XyfZiBK8.js";import{f as et}from"./Switch-Ds5Ez-CQ.js";import{a as ye}from"./client-Dy0SJfJf.js";import{c as wo,o as Co,S as So,a as $o,f as To,b as Po}from"./pwa-D67SPSmu.js";let tt=[];const Fn=new WeakMap;function zo(){tt.forEach(e=>e(...Fn.get(e))),tt=[]}function _o(e,...t){Fn.set(e,t),!tt.includes(e)&&tt.push(e)===1&&requestAnimationFrame(zo)}function Eo(e){const t=M(!!e.value);if(t.value)return Gt(t);const n=ie(e,r=>{r&&(t.value=!0,n())});return Gt(t)}function ds(){return Mn()!==null}const ko=typeof window<"u";let ke,He;const Io=()=>{var e,t;ke=ko?(t=(e=document)===null||e===void 0?void 0:e.fonts)===null||t===void 0?void 0:t.ready:void 0,He=!1,ke!==void 0?ke.then(()=>{He=!0}):He=!0};Io();function Wn(e){if(He)return;let t=!1;Ae(()=>{He||ke==null||ke.then(()=>{t||e()})}),Oe(()=>{t=!0})}function $t(e,t){return K(()=>{for(const n of t)if(e[n]!==void 0)return e[n];return e[t[t.length-1]]})}const cs=ge("n-internal-select-menu"),Bo=ge("n-internal-select-menu-body"),Rn=ge("n-drawer-body"),Hn=ge("n-modal-body"),Dn=ge("n-popover-body"),Nn="__disabled__";function Be(e){const t=Q(Hn,null),n=Q(Rn,null),r=Q(Dn,null),o=Q(Bo,null),a=M();if(typeof document<"u"){a.value=document.fullscreenElement;const s=()=>{a.value=document.fullscreenElement};Ae(()=>{se("fullscreenchange",document,s)}),Oe(()=>{ee("fullscreenchange",document,s)})}return De(()=>{var s;const{to:l}=e;return l!==void 0?l===!1?Nn:l===!0?a.value||"body":l:t!=null&&t.value?(s=t.value.$el)!==null&&s!==void 0?s:t.value:n!=null&&n.value?n.value:r!=null&&r.value?r.value:o!=null&&o.value?o.value:l??(a.value||"body")})}Be.tdkey=Nn;Be.propTo={type:[String,Object,Boolean],default:void 0};function Tt(e,t,n="default"){const r=t[n];if(r===void 0)throw new Error(`[vueuc/${e}]: slot[${n}] is empty.`);return r()}function Pt(e,t=!0,n=[]){return e.forEach(r=>{if(r!==null){if(typeof r!="object"){(typeof r=="string"||typeof r=="number")&&n.push(ho(String(r)));return}if(Array.isArray(r)){Pt(r,t,n);return}if(r.type===at){if(r.children===null)return;Array.isArray(r.children)&&Pt(r.children,t,n)}else r.type!==po&&n.push(r)}}),n}function Jt(e,t,n="default"){const r=t[n];if(r===void 0)throw new Error(`[vueuc/${e}]: slot[${n}] is empty.`);const o=Pt(r());if(o.length===1)return o[0];throw new Error(`[vueuc/${e}]: slot[${n}] should have exactly one child.`)}let fe=null;function jn(){if(fe===null&&(fe=document.getElementById("v-binder-view-measurer"),fe===null)){fe=document.createElement("div"),fe.id="v-binder-view-measurer";const{style:e}=fe;e.position="fixed",e.left="0",e.right="0",e.top="0",e.bottom="0",e.pointerEvents="none",e.visibility="hidden",document.body.appendChild(fe)}return fe.getBoundingClientRect()}function Mo(e,t){const n=jn();return{top:t,left:e,height:0,width:0,right:n.width-e,bottom:n.height-t}}function ht(e){const t=e.getBoundingClientRect(),n=jn();return{left:t.left-n.left,top:t.top-n.top,bottom:n.height+n.top-t.bottom,right:n.width+n.left-t.right,width:t.width,height:t.height}}function Ao(e){return e.nodeType===9?null:e.parentNode}function Un(e){if(e===null)return null;const t=Ao(e);if(t===null)return null;if(t.nodeType===9)return document;if(t.nodeType===1){const{overflow:n,overflowX:r,overflowY:o}=getComputedStyle(t);if(/(auto|scroll|overlay)/.test(n+o+r))return t}return Un(t)}const Oo=G({name:"Binder",props:{syncTargetWithParent:Boolean,syncTarget:{type:Boolean,default:!0}},setup(e){var t;ve("VBinder",(t=Mn())===null||t===void 0?void 0:t.proxy);const n=Q("VBinder",null),r=M(null),o=c=>{r.value=c,n&&e.syncTargetWithParent&&n.setTargetRef(c)};let a=[];const s=()=>{let c=r.value;for(;c=Un(c),c!==null;)a.push(c);for(const T of a)se("scroll",T,w,!0)},l=()=>{for(const c of a)ee("scroll",c,w,!0);a=[]},i=new Set,h=c=>{i.size===0&&s(),i.has(c)||i.add(c)},g=c=>{i.has(c)&&i.delete(c),i.size===0&&l()},w=()=>{_o(u)},u=()=>{i.forEach(c=>c())},d=new Set,x=c=>{d.size===0&&se("resize",window,v),d.has(c)||d.add(c)},b=c=>{d.has(c)&&d.delete(c),d.size===0&&ee("resize",window,v)},v=()=>{d.forEach(c=>c())};return Oe(()=>{ee("resize",window,v),l()}),{targetRef:r,setTargetRef:o,addScrollListener:h,removeScrollListener:g,addResizeListener:x,removeResizeListener:b}},render(){return Tt("binder",this.$slots)}}),Lo=G({name:"Target",setup(){const{setTargetRef:e,syncTarget:t}=Q("VBinder");return{syncTarget:t,setTargetDirective:{mounted:e,updated:e}}},render(){const{syncTarget:e,setTargetDirective:t}=this;return e?Ve(Jt("follower",this.$slots),[[t]]):Jt("follower",this.$slots)}}),Pe="@@mmoContext",Fo={mounted(e,{value:t}){e[Pe]={handler:void 0},typeof t=="function"&&(e[Pe].handler=t,se("mousemoveoutside",e,t))},updated(e,{value:t}){const n=e[Pe];typeof t=="function"?n.handler?n.handler!==t&&(ee("mousemoveoutside",e,n.handler),n.handler=t,se("mousemoveoutside",e,t)):(e[Pe].handler=t,se("mousemoveoutside",e,t)):n.handler&&(ee("mousemoveoutside",e,n.handler),n.handler=void 0)},unmounted(e){const{handler:t}=e[Pe];t&&ee("mousemoveoutside",e,t),e[Pe].handler=void 0}},ze="@@coContext",Qt={mounted(e,{value:t,modifiers:n}){e[ze]={handler:void 0},typeof t=="function"&&(e[ze].handler=t,se("clickoutside",e,t,{capture:n.capture}))},updated(e,{value:t,modifiers:n}){const r=e[ze];typeof t=="function"?r.handler?r.handler!==t&&(ee("clickoutside",e,r.handler,{capture:n.capture}),r.handler=t,se("clickoutside",e,t,{capture:n.capture})):(e[ze].handler=t,se("clickoutside",e,t,{capture:n.capture})):r.handler&&(ee("clickoutside",e,r.handler,{capture:n.capture}),r.handler=void 0)},unmounted(e,{modifiers:t}){const{handler:n}=e[ze];n&&ee("clickoutside",e,n,{capture:t.capture}),e[ze].handler=void 0}};function Wo(e,t){console.error(`[vdirs/${e}]: ${t}`)}class Ro{constructor(){this.elementZIndex=new Map,this.nextZIndex=2e3}get elementCount(){return this.elementZIndex.size}ensureZIndex(t,n){const{elementZIndex:r}=this;if(n!==void 0){t.style.zIndex=`${n}`,r.delete(t);return}const{nextZIndex:o}=this;r.has(t)&&r.get(t)+1===this.nextZIndex||(t.style.zIndex=`${o}`,r.set(t,o),this.nextZIndex=o+1,this.squashState())}unregister(t,n){const{elementZIndex:r}=this;r.has(t)?r.delete(t):n===void 0&&Wo("z-index-manager/unregister-element","Element not found when unregistering."),this.squashState()}squashState(){const{elementCount:t}=this;t||(this.nextZIndex=2e3),this.nextZIndex-t>2500&&this.rearrange()}rearrange(){const t=Array.from(this.elementZIndex.entries());t.sort((n,r)=>n[1]-r[1]),this.nextZIndex=2e3,t.forEach(n=>{const r=n[0],o=this.nextZIndex++;`${o}`!==r.style.zIndex&&(r.style.zIndex=`${o}`)})}}const pt=new Ro,_e="@@ziContext",Vn={mounted(e,t){const{value:n={}}=t,{zIndex:r,enabled:o}=n;e[_e]={enabled:!!o,initialized:!1},o&&(pt.ensureZIndex(e,r),e[_e].initialized=!0)},updated(e,t){const{value:n={}}=t,{zIndex:r,enabled:o}=n,a=e[_e].enabled;o&&!a&&(pt.ensureZIndex(e,r),e[_e].initialized=!0),e[_e].enabled=!!o},unmounted(e,t){if(!e[_e].initialized)return;const{value:n={}}=t,{zIndex:r}=n;pt.unregister(e,r)}},{c:Ee}=Ar(),Xn="vueuc-style";function en(e){return typeof e=="string"?document.querySelector(e):e()||null}const Ho=G({name:"LazyTeleport",props:{to:{type:[String,Object],default:void 0},disabled:Boolean,show:{type:Boolean,required:!0}},setup(e){return{showTeleport:Eo(D(e,"show")),mergedTo:K(()=>{const{to:t}=e;return t??"body"})}},render(){return this.showTeleport?this.disabled?Tt("lazy-teleport",this.$slots):y(bo,{disabled:this.disabled,to:this.mergedTo},Tt("lazy-teleport",this.$slots)):null}}),Ze={top:"bottom",bottom:"top",left:"right",right:"left"},tn={start:"end",center:"center",end:"start"},bt={top:"height",bottom:"height",left:"width",right:"width"},Do={"bottom-start":"top left",bottom:"top center","bottom-end":"top right","top-start":"bottom left",top:"bottom center","top-end":"bottom right","right-start":"top left",right:"center left","right-end":"bottom left","left-start":"top right",left:"center right","left-end":"bottom right"},No={"bottom-start":"bottom left",bottom:"bottom center","bottom-end":"bottom right","top-start":"top left",top:"top center","top-end":"top right","right-start":"top right",right:"center right","right-end":"bottom right","left-start":"top left",left:"center left","left-end":"bottom left"},jo={"bottom-start":"right","bottom-end":"left","top-start":"right","top-end":"left","right-start":"bottom","right-end":"top","left-start":"bottom","left-end":"top"},nn={top:!0,bottom:!1,left:!0,right:!1},rn={top:"end",bottom:"start",left:"end",right:"start"};function Uo(e,t,n,r,o,a){if(!o||a)return{placement:e,top:0,left:0};const[s,l]=e.split("-");let i=l??"center",h={top:0,left:0};const g=(d,x,b)=>{let v=0,c=0;const T=n[d]-t[x]-t[d];return T>0&&r&&(b?c=nn[x]?T:-T:v=nn[x]?T:-T),{left:v,top:c}},w=s==="left"||s==="right";if(i!=="center"){const d=jo[e],x=Ze[d],b=bt[d];if(n[b]>t[b]){if(t[d]+t[b]<n[b]){const v=(n[b]-t[b])/2;t[d]<v||t[x]<v?t[d]<t[x]?(i=tn[l],h=g(b,x,w)):h=g(b,d,w):i="center"}}else n[b]<t[b]&&t[x]<0&&t[d]>t[x]&&(i=tn[l])}else{const d=s==="bottom"||s==="top"?"left":"top",x=Ze[d],b=bt[d],v=(n[b]-t[b])/2;(t[d]<v||t[x]<v)&&(t[d]>t[x]?(i=rn[d],h=g(b,d,w)):(i=rn[x],h=g(b,x,w)))}let u=s;return t[s]<n[bt[s]]&&t[s]<t[Ze[s]]&&(u=Ze[s]),{placement:i!=="center"?`${u}-${i}`:u,left:h.left,top:h.top}}function Vo(e,t){return t?No[e]:Do[e]}function Xo(e,t,n,r,o,a){if(a)switch(e){case"bottom-start":return{top:`${Math.round(n.top-t.top+n.height)}px`,left:`${Math.round(n.left-t.left)}px`,transform:"translateY(-100%)"};case"bottom-end":return{top:`${Math.round(n.top-t.top+n.height)}px`,left:`${Math.round(n.left-t.left+n.width)}px`,transform:"translateX(-100%) translateY(-100%)"};case"top-start":return{top:`${Math.round(n.top-t.top)}px`,left:`${Math.round(n.left-t.left)}px`,transform:""};case"top-end":return{top:`${Math.round(n.top-t.top)}px`,left:`${Math.round(n.left-t.left+n.width)}px`,transform:"translateX(-100%)"};case"right-start":return{top:`${Math.round(n.top-t.top)}px`,left:`${Math.round(n.left-t.left+n.width)}px`,transform:"translateX(-100%)"};case"right-end":return{top:`${Math.round(n.top-t.top+n.height)}px`,left:`${Math.round(n.left-t.left+n.width)}px`,transform:"translateX(-100%) translateY(-100%)"};case"left-start":return{top:`${Math.round(n.top-t.top)}px`,left:`${Math.round(n.left-t.left)}px`,transform:""};case"left-end":return{top:`${Math.round(n.top-t.top+n.height)}px`,left:`${Math.round(n.left-t.left)}px`,transform:"translateY(-100%)"};case"top":return{top:`${Math.round(n.top-t.top)}px`,left:`${Math.round(n.left-t.left+n.width/2)}px`,transform:"translateX(-50%)"};case"right":return{top:`${Math.round(n.top-t.top+n.height/2)}px`,left:`${Math.round(n.left-t.left+n.width)}px`,transform:"translateX(-100%) translateY(-50%)"};case"left":return{top:`${Math.round(n.top-t.top+n.height/2)}px`,left:`${Math.round(n.left-t.left)}px`,transform:"translateY(-50%)"};case"bottom":default:return{top:`${Math.round(n.top-t.top+n.height)}px`,left:`${Math.round(n.left-t.left+n.width/2)}px`,transform:"translateX(-50%) translateY(-100%)"}}switch(e){case"bottom-start":return{top:`${Math.round(n.top-t.top+n.height+r)}px`,left:`${Math.round(n.left-t.left+o)}px`,transform:""};case"bottom-end":return{top:`${Math.round(n.top-t.top+n.height+r)}px`,left:`${Math.round(n.left-t.left+n.width+o)}px`,transform:"translateX(-100%)"};case"top-start":return{top:`${Math.round(n.top-t.top+r)}px`,left:`${Math.round(n.left-t.left+o)}px`,transform:"translateY(-100%)"};case"top-end":return{top:`${Math.round(n.top-t.top+r)}px`,left:`${Math.round(n.left-t.left+n.width+o)}px`,transform:"translateX(-100%) translateY(-100%)"};case"right-start":return{top:`${Math.round(n.top-t.top+r)}px`,left:`${Math.round(n.left-t.left+n.width+o)}px`,transform:""};case"right-end":return{top:`${Math.round(n.top-t.top+n.height+r)}px`,left:`${Math.round(n.left-t.left+n.width+o)}px`,transform:"translateY(-100%)"};case"left-start":return{top:`${Math.round(n.top-t.top+r)}px`,left:`${Math.round(n.left-t.left+o)}px`,transform:"translateX(-100%)"};case"left-end":return{top:`${Math.round(n.top-t.top+n.height+r)}px`,left:`${Math.round(n.left-t.left+o)}px`,transform:"translateX(-100%) translateY(-100%)"};case"top":return{top:`${Math.round(n.top-t.top+r)}px`,left:`${Math.round(n.left-t.left+n.width/2+o)}px`,transform:"translateY(-100%) translateX(-50%)"};case"right":return{top:`${Math.round(n.top-t.top+n.height/2+r)}px`,left:`${Math.round(n.left-t.left+n.width+o)}px`,transform:"translateY(-50%)"};case"left":return{top:`${Math.round(n.top-t.top+n.height/2+r)}px`,left:`${Math.round(n.left-t.left+o)}px`,transform:"translateY(-50%) translateX(-100%)"};case"bottom":default:return{top:`${Math.round(n.top-t.top+n.height+r)}px`,left:`${Math.round(n.left-t.left+n.width/2+o)}px`,transform:"translateX(-50%)"}}}const Ko=Ee([Ee(".v-binder-follower-container",{position:"absolute",left:"0",right:"0",top:"0",height:"0",pointerEvents:"none",zIndex:"auto"}),Ee(".v-binder-follower-content",{position:"absolute",zIndex:"auto"},[Ee("> *",{pointerEvents:"all"})])]),Yo=G({name:"Follower",inheritAttrs:!1,props:{show:Boolean,enabled:{type:Boolean,default:void 0},placement:{type:String,default:"bottom"},syncTrigger:{type:Array,default:["resize","scroll"]},to:[String,Object],flip:{type:Boolean,default:!0},internalShift:Boolean,x:Number,y:Number,width:String,minWidth:String,containerClass:String,teleportDisabled:Boolean,zindexable:{type:Boolean,default:!0},zIndex:Number,overlap:Boolean},setup(e){const t=Q("VBinder"),n=De(()=>e.enabled!==void 0?e.enabled:e.show),r=M(null),o=M(null),a=()=>{const{syncTrigger:u}=e;u.includes("scroll")&&t.addScrollListener(i),u.includes("resize")&&t.addResizeListener(i)},s=()=>{t.removeScrollListener(i),t.removeResizeListener(i)};Ae(()=>{n.value&&(i(),a())});const l=$n();Ko.mount({id:"vueuc/binder",head:!0,anchorMetaName:Xn,ssr:l}),Oe(()=>{s()}),Wn(()=>{n.value&&i()});const i=()=>{if(!n.value)return;const u=r.value;if(u===null)return;const d=t.targetRef,{x,y:b,overlap:v}=e,c=x!==void 0&&b!==void 0?Mo(x,b):ht(d);u.style.setProperty("--v-target-width",`${Math.round(c.width)}px`),u.style.setProperty("--v-target-height",`${Math.round(c.height)}px`);const{width:T,minWidth:L,placement:B,internalShift:_,flip:z}=e;u.setAttribute("v-placement",B),v?u.setAttribute("v-overlap",""):u.removeAttribute("v-overlap");const{style:S}=u;T==="target"?S.width=`${c.width}px`:T!==void 0?S.width=T:S.width="",L==="target"?S.minWidth=`${c.width}px`:L!==void 0?S.minWidth=L:S.minWidth="";const A=ht(u),F=ht(o.value),{left:E,top:V,placement:Y}=Uo(B,c,A,_,z,v),N=Vo(Y,v),{left:q,top:C,transform:R}=Xo(Y,F,c,V,E,v);u.setAttribute("v-placement",Y),u.style.setProperty("--v-offset-left",`${Math.round(E)}px`),u.style.setProperty("--v-offset-top",`${Math.round(V)}px`),u.style.transform=`translateX(${q}) translateY(${C}) ${R}`,u.style.setProperty("--v-transform-origin",N),u.style.transformOrigin=N};ie(n,u=>{u?(a(),h()):s()});const h=()=>{Qe().then(i).catch(u=>console.error(u))};["placement","x","y","internalShift","flip","width","overlap","minWidth"].forEach(u=>{ie(D(e,u),i)}),["teleportDisabled"].forEach(u=>{ie(D(e,u),h)}),ie(D(e,"syncTrigger"),u=>{u.includes("resize")?t.addResizeListener(i):t.removeResizeListener(i),u.includes("scroll")?t.addScrollListener(i):t.removeScrollListener(i)});const g=Tn(),w=De(()=>{const{to:u}=e;if(u!==void 0)return u;g.value});return{VBinder:t,mergedEnabled:n,offsetContainerRef:o,followerRef:r,mergedTo:w,syncPosition:i}},render(){return y(Ho,{show:this.show,to:this.mergedTo,disabled:this.teleportDisabled},{default:()=>{var e,t;const n=y("div",{class:["v-binder-follower-container",this.containerClass],ref:"offsetContainerRef"},[y("div",{class:"v-binder-follower-content",ref:"followerRef"},(t=(e=this.$slots).default)===null||t===void 0?void 0:t.call(e))]);return this.zindexable?Ve(n,[[Vn,{enabled:this.mergedEnabled,zIndex:this.zIndex}]]):n}})}}),Go=Ee(".v-x-scroll",{overflow:"auto",scrollbarWidth:"none"},[Ee("&::-webkit-scrollbar",{width:0,height:0})]),Zo=G({name:"XScroll",props:{disabled:Boolean,onScroll:Function},setup(){const e=M(null);function t(o){!(o.currentTarget.offsetWidth<o.currentTarget.scrollWidth)||o.deltaY===0||(o.currentTarget.scrollLeft+=o.deltaY+o.deltaX,o.preventDefault())}const n=$n();return Go.mount({id:"vueuc/x-scroll",head:!0,anchorMetaName:Xn,ssr:n}),Object.assign({selfRef:e,handleWheel:t},{scrollTo(...o){var a;(a=e.value)===null||a===void 0||a.scrollTo(...o)}})},render(){return y("div",{ref:"selfRef",onScroll:this.onScroll,onWheel:this.disabled?void 0:this.handleWheel,class:"v-x-scroll"},this.$slots)}});function Kn(e){return e instanceof HTMLElement}function Yn(e){for(let t=0;t<e.childNodes.length;t++){const n=e.childNodes[t];if(Kn(n)&&(Zn(n)||Yn(n)))return!0}return!1}function Gn(e){for(let t=e.childNodes.length-1;t>=0;t--){const n=e.childNodes[t];if(Kn(n)&&(Zn(n)||Gn(n)))return!0}return!1}function Zn(e){if(!qo(e))return!1;try{e.focus({preventScroll:!0})}catch{}return document.activeElement===e}function qo(e){if(e.tabIndex>0||e.tabIndex===0&&e.getAttribute("tabIndex")!==null)return!0;if(e.getAttribute("disabled"))return!1;switch(e.nodeName){case"A":return!!e.href&&e.rel!=="ignore";case"INPUT":return e.type!=="hidden"&&e.type!=="file";case"SELECT":case"TEXTAREA":return!0;default:return!1}}let We=[];const Jo=G({name:"FocusTrap",props:{disabled:Boolean,active:Boolean,autoFocus:{type:Boolean,default:!0},onEsc:Function,initialFocusTo:[String,Function],finalFocusTo:[String,Function],returnFocusOnDeactivated:{type:Boolean,default:!0}},setup(e){const t=Or(),n=M(null),r=M(null);let o=!1,a=!1;const s=typeof document>"u"?null:document.activeElement;function l(){return We[We.length-1]===t}function i(v){var c;v.code==="Escape"&&l()&&((c=e.onEsc)===null||c===void 0||c.call(e,v))}Ae(()=>{ie(()=>e.active,v=>{v?(w(),se("keydown",document,i)):(ee("keydown",document,i),o&&u())},{immediate:!0})}),Oe(()=>{ee("keydown",document,i),o&&u()});function h(v){if(!a&&l()){const c=g();if(c===null||c.contains(Ct(v)))return;d("first")}}function g(){const v=n.value;if(v===null)return null;let c=v;for(;c=c.nextSibling,!(c===null||c instanceof Element&&c.tagName==="DIV"););return c}function w(){var v;if(!e.disabled){if(We.push(t),e.autoFocus){const{initialFocusTo:c}=e;c===void 0?d("first"):(v=en(c))===null||v===void 0||v.focus({preventScroll:!0})}o=!0,document.addEventListener("focus",h,!0)}}function u(){var v;if(e.disabled||(document.removeEventListener("focus",h,!0),We=We.filter(T=>T!==t),l()))return;const{finalFocusTo:c}=e;c!==void 0?(v=en(c))===null||v===void 0||v.focus({preventScroll:!0}):e.returnFocusOnDeactivated&&s instanceof HTMLElement&&(a=!0,s.focus({preventScroll:!0}),a=!1)}function d(v){if(l()&&e.active){const c=n.value,T=r.value;if(c!==null&&T!==null){const L=g();if(L==null||L===T){a=!0,c.focus({preventScroll:!0}),a=!1;return}a=!0;const B=v==="first"?Yn(L):Gn(L);a=!1,B||(a=!0,c.focus({preventScroll:!0}),a=!1)}}}function x(v){if(a)return;const c=g();c!==null&&(v.relatedTarget!==null&&c.contains(v.relatedTarget)?d("last"):d("first"))}function b(v){a||(v.relatedTarget!==null&&v.relatedTarget===n.value?d("last"):d("first"))}return{focusableStartRef:n,focusableEndRef:r,focusableStyle:"position: absolute; height: 0; width: 0;",handleStartFocus:x,handleEndFocus:b}},render(){const{default:e}=this.$slots;if(e===void 0)return null;if(this.disabled)return e();const{active:t,focusableStyle:n}=this;return y(at,null,[y("div",{"aria-hidden":"true",tabindex:t?"0":"-1",ref:"focusableStartRef",style:n,onFocus:this.handleStartFocus}),e(),y("div",{"aria-hidden":"true",style:n,ref:"focusableEndRef",tabindex:t?"0":"-1",onFocus:this.handleEndFocus})])}});let vt;function Qo(){return vt===void 0&&(vt=navigator.userAgent.includes("Node.js")||navigator.userAgent.includes("jsdom")),vt}function on(e,t="default",n=void 0){const r=e[t];if(!r)return Rt("getFirstSlotVNode",`slot[${t}] is empty`),null;const o=et(r(n));return o.length===1?o[0]:(Rt("getFirstSlotVNode",`slot[${t}] should have exactly one child`),null)}function qn(e,t=[],n){const r={};return t.forEach(o=>{r[o]=e[o]}),Object.assign(r,n)}var ea=/\s/;function ta(e){for(var t=e.length;t--&&ea.test(e.charAt(t)););return t}var na=/^\s+/;function ra(e){return e&&e.slice(0,ta(e)+1).replace(na,"")}var an=NaN,oa=/^[-+]0x[0-9a-f]+$/i,aa=/^0b[01]+$/i,ia=/^0o[0-7]+$/i,sa=parseInt;function sn(e){if(typeof e=="number")return e;if(Lr(e))return an;if(Ne(e)){var t=typeof e.valueOf=="function"?e.valueOf():e;e=Ne(t)?t+"":t}if(typeof e!="string")return e===0?e:+e;e=ra(e);var n=aa.test(e);return n||ia.test(e)?sa(e.slice(2),n?2:8):oa.test(e)?an:+e}var zt=rt(je,"WeakMap"),la=Fr(Object.keys,Object),da=Object.prototype,ca=da.hasOwnProperty;function ua(e){if(!Wr(e))return la(e);var t=[];for(var n in Object(e))ca.call(e,n)&&n!="constructor"&&t.push(n);return t}function Ot(e){return Bt(e)?Rr(e):ua(e)}function fa(e,t){for(var n=-1,r=t.length,o=e.length;++n<r;)e[o+n]=t[n];return e}function ha(e,t){for(var n=-1,r=e==null?0:e.length,o=0,a=[];++n<r;){var s=e[n];t(s,n,e)&&(a[o++]=s)}return a}function pa(){return[]}var ba=Object.prototype,va=ba.propertyIsEnumerable,ln=Object.getOwnPropertySymbols,ga=ln?function(e){return e==null?[]:(e=Object(e),ha(ln(e),function(t){return va.call(e,t)}))}:pa;function ma(e,t,n){var r=t(e);return Ie(e)?r:fa(r,n(e))}function dn(e){return ma(e,Ot,ga)}var _t=rt(je,"DataView"),Et=rt(je,"Promise"),kt=rt(je,"Set"),cn="[object Map]",ya="[object Object]",un="[object Promise]",fn="[object Set]",hn="[object WeakMap]",pn="[object DataView]",xa=Me(_t),wa=Me(St),Ca=Me(Et),Sa=Me(kt),$a=Me(zt),he=Pn;(_t&&he(new _t(new ArrayBuffer(1)))!=pn||St&&he(new St)!=cn||Et&&he(Et.resolve())!=un||kt&&he(new kt)!=fn||zt&&he(new zt)!=hn)&&(he=function(e){var t=Pn(e),n=t==ya?e.constructor:void 0,r=n?Me(n):"";if(r)switch(r){case xa:return pn;case wa:return cn;case Ca:return un;case Sa:return fn;case $a:return hn}return t});var Ta="__lodash_hash_undefined__";function Pa(e){return this.__data__.set(e,Ta),this}function za(e){return this.__data__.has(e)}function nt(e){var t=-1,n=e==null?0:e.length;for(this.__data__=new Hr;++t<n;)this.add(e[t])}nt.prototype.add=nt.prototype.push=Pa;nt.prototype.has=za;function _a(e,t){for(var n=-1,r=e==null?0:e.length;++n<r;)if(t(e[n],n,e))return!0;return!1}function Ea(e,t){return e.has(t)}var ka=1,Ia=2;function Jn(e,t,n,r,o,a){var s=n&ka,l=e.length,i=t.length;if(l!=i&&!(s&&i>l))return!1;var h=a.get(e),g=a.get(t);if(h&&g)return h==t&&g==e;var w=-1,u=!0,d=n&Ia?new nt:void 0;for(a.set(e,t),a.set(t,e);++w<l;){var x=e[w],b=t[w];if(r)var v=s?r(b,x,w,t,e,a):r(x,b,w,e,t,a);if(v!==void 0){if(v)continue;u=!1;break}if(d){if(!_a(t,function(c,T){if(!Ea(d,T)&&(x===c||o(x,c,n,r,a)))return d.push(T)})){u=!1;break}}else if(!(x===b||o(x,b,n,r,a))){u=!1;break}}return a.delete(e),a.delete(t),u}function Ba(e){var t=-1,n=Array(e.size);return e.forEach(function(r,o){n[++t]=[o,r]}),n}function Ma(e){var t=-1,n=Array(e.size);return e.forEach(function(r){n[++t]=r}),n}var Aa=1,Oa=2,La="[object Boolean]",Fa="[object Date]",Wa="[object Error]",Ra="[object Map]",Ha="[object Number]",Da="[object RegExp]",Na="[object Set]",ja="[object String]",Ua="[object Symbol]",Va="[object ArrayBuffer]",Xa="[object DataView]",bn=Ht?Ht.prototype:void 0,gt=bn?bn.valueOf:void 0;function Ka(e,t,n,r,o,a,s){switch(n){case Xa:if(e.byteLength!=t.byteLength||e.byteOffset!=t.byteOffset)return!1;e=e.buffer,t=t.buffer;case Va:return!(e.byteLength!=t.byteLength||!a(new Dt(e),new Dt(t)));case La:case Fa:case Ha:return Dr(+e,+t);case Wa:return e.name==t.name&&e.message==t.message;case Da:case ja:return e==t+"";case Ra:var l=Ba;case Na:var i=r&Aa;if(l||(l=Ma),e.size!=t.size&&!i)return!1;var h=s.get(e);if(h)return h==t;r|=Oa,s.set(e,t);var g=Jn(l(e),l(t),r,o,a,s);return s.delete(e),g;case Ua:if(gt)return gt.call(e)==gt.call(t)}return!1}var Ya=1,Ga=Object.prototype,Za=Ga.hasOwnProperty;function qa(e,t,n,r,o,a){var s=n&Ya,l=dn(e),i=l.length,h=dn(t),g=h.length;if(i!=g&&!s)return!1;for(var w=i;w--;){var u=l[w];if(!(s?u in t:Za.call(t,u)))return!1}var d=a.get(e),x=a.get(t);if(d&&x)return d==t&&x==e;var b=!0;a.set(e,t),a.set(t,e);for(var v=s;++w<i;){u=l[w];var c=e[u],T=t[u];if(r)var L=s?r(T,c,u,t,e,a):r(c,T,u,e,t,a);if(!(L===void 0?c===T||o(c,T,n,r,a):L)){b=!1;break}v||(v=u=="constructor")}if(b&&!v){var B=e.constructor,_=t.constructor;B!=_&&"constructor"in e&&"constructor"in t&&!(typeof B=="function"&&B instanceof B&&typeof _=="function"&&_ instanceof _)&&(b=!1)}return a.delete(e),a.delete(t),b}var Ja=1,vn="[object Arguments]",gn="[object Array]",qe="[object Object]",Qa=Object.prototype,mn=Qa.hasOwnProperty;function ei(e,t,n,r,o,a){var s=Ie(e),l=Ie(t),i=s?gn:he(e),h=l?gn:he(t);i=i==vn?qe:i,h=h==vn?qe:h;var g=i==qe,w=h==qe,u=i==h;if(u&&Nt(e)){if(!Nt(t))return!1;s=!0,g=!1}if(u&&!g)return a||(a=new Je),s||Nr(e)?Jn(e,t,n,r,o,a):Ka(e,t,i,n,r,o,a);if(!(n&Ja)){var d=g&&mn.call(e,"__wrapped__"),x=w&&mn.call(t,"__wrapped__");if(d||x){var b=d?e.value():e,v=x?t.value():t;return a||(a=new Je),o(b,v,n,r,a)}}return u?(a||(a=new Je),qa(e,t,n,r,o,a)):!1}function Lt(e,t,n,r,o){return e===t?!0:e==null||t==null||!jt(e)&&!jt(t)?e!==e&&t!==t:ei(e,t,n,r,Lt,o)}var ti=1,ni=2;function ri(e,t,n,r){var o=n.length,a=o;if(e==null)return!a;for(e=Object(e);o--;){var s=n[o];if(s[2]?s[1]!==e[s[0]]:!(s[0]in e))return!1}for(;++o<a;){s=n[o];var l=s[0],i=e[l],h=s[1];if(s[2]){if(i===void 0&&!(l in e))return!1}else{var g=new Je,w;if(!(w===void 0?Lt(h,i,ti|ni,r,g):w))return!1}}return!0}function Qn(e){return e===e&&!Ne(e)}function oi(e){for(var t=Ot(e),n=t.length;n--;){var r=t[n],o=e[r];t[n]=[r,o,Qn(o)]}return t}function er(e,t){return function(n){return n==null?!1:n[e]===t&&(t!==void 0||e in Object(n))}}function ai(e){var t=oi(e);return t.length==1&&t[0][2]?er(t[0][0],t[0][1]):function(n){return n===e||ri(n,e,t)}}function ii(e,t){return e!=null&&t in Object(e)}function si(e,t,n){t=jr(t,e);for(var r=-1,o=t.length,a=!1;++r<o;){var s=Mt(t[r]);if(!(a=e!=null&&n(e,s)))break;e=e[s]}return a||++r!=o?a:(o=e==null?0:e.length,!!o&&Ur(o)&&Vr(s,o)&&(Ie(e)||Xr(e)))}function li(e,t){return e!=null&&si(e,t,ii)}var di=1,ci=2;function ui(e,t){return zn(e)&&Qn(t)?er(Mt(e),t):function(n){var r=Kr(n,e);return r===void 0&&r===t?li(n,e):Lt(t,r,di|ci)}}function fi(e){return function(t){return t==null?void 0:t[e]}}function hi(e){return function(t){return Yr(t,e)}}function pi(e){return zn(e)?fi(Mt(e)):hi(e)}function bi(e){return typeof e=="function"?e:e==null?Gr:typeof e=="object"?Ie(e)?ui(e[0],e[1]):ai(e):pi(e)}function vi(e,t){return e&&Zr(e,t,Ot)}function gi(e,t){return function(n,r){if(n==null)return n;if(!Bt(n))return e(n,r);for(var o=n.length,a=-1,s=Object(n);++a<o&&r(s[a],a,s)!==!1;);return n}}var mi=gi(vi),mt=function(){return je.Date.now()},yi="Expected a function",xi=Math.max,wi=Math.min;function Ci(e,t,n){var r,o,a,s,l,i,h=0,g=!1,w=!1,u=!0;if(typeof e!="function")throw new TypeError(yi);t=sn(t)||0,Ne(n)&&(g=!!n.leading,w="maxWait"in n,a=w?xi(sn(n.maxWait)||0,t):a,u="trailing"in n?!!n.trailing:u);function d(z){var S=r,A=o;return r=o=void 0,h=z,s=e.apply(A,S),s}function x(z){return h=z,l=setTimeout(c,t),g?d(z):s}function b(z){var S=z-i,A=z-h,F=t-S;return w?wi(F,a-A):F}function v(z){var S=z-i,A=z-h;return i===void 0||S>=t||S<0||w&&A>=a}function c(){var z=mt();if(v(z))return T(z);l=setTimeout(c,b(z))}function T(z){return l=void 0,u&&r?d(z):(r=o=void 0,s)}function L(){l!==void 0&&clearTimeout(l),h=0,r=i=o=l=void 0}function B(){return l===void 0?s:T(mt())}function _(){var z=mt(),S=v(z);if(r=arguments,o=this,i=z,S){if(l===void 0)return x(i);if(w)return clearTimeout(l),l=setTimeout(c,t),d(i)}return l===void 0&&(l=setTimeout(c,t)),s}return _.cancel=L,_.flush=B,_}function Si(e,t){var n=-1,r=Bt(e)?Array(e.length):[];return mi(e,function(o,a,s){r[++n]=t(o,a,s)}),r}function $i(e,t){var n=Ie(e)?qr:Si;return n(e,bi(t))}var Ti="Expected a function";function yt(e,t,n){var r=!0,o=!0;if(typeof e!="function")throw new TypeError(Ti);return Ne(n)&&(r="leading"in n?!!n.leading:r,o="trailing"in n?!!n.trailing:o),Ci(e,t,{leading:r,maxWait:t,trailing:o})}const Pi=G({name:"Add",render(){return y("svg",{width:"512",height:"512",viewBox:"0 0 512 512",fill:"none",xmlns:"http://www.w3.org/2000/svg"},y("path",{d:"M256 112V400M400 256H112",stroke:"currentColor","stroke-width":"32","stroke-linecap":"round","stroke-linejoin":"round"}))}}),xt={top:"bottom",bottom:"top",left:"right",right:"left"},U="var(--n-arrow-height) * 1.414",zi=I([m("popover",`
 transition:
 box-shadow .3s var(--n-bezier),
 background-color .3s var(--n-bezier),
 color .3s var(--n-bezier);
 position: relative;
 font-size: var(--n-font-size);
 color: var(--n-text-color);
 box-shadow: var(--n-box-shadow);
 word-break: break-word;
 `,[I(">",[m("scrollbar",`
 height: inherit;
 max-height: inherit;
 `)]),pe("raw",`
 background-color: var(--n-color);
 border-radius: var(--n-border-radius);
 `,[pe("scrollable",[pe("show-header-or-footer","padding: var(--n-padding);")])]),O("header",`
 padding: var(--n-padding);
 border-bottom: 1px solid var(--n-divider-color);
 transition: border-color .3s var(--n-bezier);
 `),O("footer",`
 padding: var(--n-padding);
 border-top: 1px solid var(--n-divider-color);
 transition: border-color .3s var(--n-bezier);
 `),$("scrollable, show-header-or-footer",[O("content",`
 padding: var(--n-padding);
 `)])]),m("popover-shared",`
 transform-origin: inherit;
 `,[m("popover-arrow-wrapper",`
 position: absolute;
 overflow: hidden;
 pointer-events: none;
 `,[m("popover-arrow",`
 transition: background-color .3s var(--n-bezier);
 position: absolute;
 display: block;
 width: calc(${U});
 height: calc(${U});
 box-shadow: 0 0 8px 0 rgba(0, 0, 0, .12);
 transform: rotate(45deg);
 background-color: var(--n-color);
 pointer-events: all;
 `)]),I("&.popover-transition-enter-from, &.popover-transition-leave-to",`
 opacity: 0;
 transform: scale(.85);
 `),I("&.popover-transition-enter-to, &.popover-transition-leave-from",`
 transform: scale(1);
 opacity: 1;
 `),I("&.popover-transition-enter-active",`
 transition:
 box-shadow .3s var(--n-bezier),
 background-color .3s var(--n-bezier),
 color .3s var(--n-bezier),
 opacity .15s var(--n-bezier-ease-out),
 transform .15s var(--n-bezier-ease-out);
 `),I("&.popover-transition-leave-active",`
 transition:
 box-shadow .3s var(--n-bezier),
 background-color .3s var(--n-bezier),
 color .3s var(--n-bezier),
 opacity .15s var(--n-bezier-ease-in),
 transform .15s var(--n-bezier-ease-in);
 `)]),J("top-start",`
 top: calc(${U} / -2);
 left: calc(${le("top-start")} - var(--v-offset-left));
 `),J("top",`
 top: calc(${U} / -2);
 transform: translateX(calc(${U} / -2)) rotate(45deg);
 left: 50%;
 `),J("top-end",`
 top: calc(${U} / -2);
 right: calc(${le("top-end")} + var(--v-offset-left));
 `),J("bottom-start",`
 bottom: calc(${U} / -2);
 left: calc(${le("bottom-start")} - var(--v-offset-left));
 `),J("bottom",`
 bottom: calc(${U} / -2);
 transform: translateX(calc(${U} / -2)) rotate(45deg);
 left: 50%;
 `),J("bottom-end",`
 bottom: calc(${U} / -2);
 right: calc(${le("bottom-end")} + var(--v-offset-left));
 `),J("left-start",`
 left: calc(${U} / -2);
 top: calc(${le("left-start")} - var(--v-offset-top));
 `),J("left",`
 left: calc(${U} / -2);
 transform: translateY(calc(${U} / -2)) rotate(45deg);
 top: 50%;
 `),J("left-end",`
 left: calc(${U} / -2);
 bottom: calc(${le("left-end")} + var(--v-offset-top));
 `),J("right-start",`
 right: calc(${U} / -2);
 top: calc(${le("right-start")} - var(--v-offset-top));
 `),J("right",`
 right: calc(${U} / -2);
 transform: translateY(calc(${U} / -2)) rotate(45deg);
 top: 50%;
 `),J("right-end",`
 right: calc(${U} / -2);
 bottom: calc(${le("right-end")} + var(--v-offset-top));
 `),...$i({top:["right-start","left-start"],right:["top-end","bottom-end"],bottom:["right-end","left-end"],left:["top-start","bottom-start"]},(e,t)=>{const n=["right","left"].includes(t),r=n?"width":"height";return e.map(o=>{const a=o.split("-")[1]==="end",l=`calc((${`var(--v-target-${r}, 0px)`} - ${U}) / 2)`,i=le(o);return I(`[v-placement="${o}"] >`,[m("popover-shared",[$("center-arrow",[m("popover-arrow",`${t}: calc(max(${l}, ${i}) ${a?"+":"-"} var(--v-offset-${n?"left":"top"}));`)])])])})})]);function le(e){return["top","bottom"].includes(e.split("-")[0])?"var(--n-arrow-offset)":"var(--n-arrow-offset-vertical)"}function J(e,t){const n=e.split("-")[0],r=["top","bottom"].includes(n)?"height: var(--n-space-arrow);":"width: var(--n-space-arrow);";return I(`[v-placement="${e}"] >`,[m("popover-shared",`
 margin-${xt[n]}: var(--n-space);
 `,[$("show-arrow",`
 margin-${xt[n]}: var(--n-space-arrow);
 `),$("overlap",`
 margin: 0;
 `),Jr("popover-arrow-wrapper",`
 right: 0;
 left: 0;
 top: 0;
 bottom: 0;
 ${n}: 100%;
 ${xt[n]}: auto;
 ${r}
 `,[m("popover-arrow",t)])])])}const tr=Object.assign(Object.assign({},de.props),{to:Be.propTo,show:Boolean,trigger:String,showArrow:Boolean,delay:Number,duration:Number,raw:Boolean,arrowPointToCenter:Boolean,arrowClass:String,arrowStyle:[String,Object],arrowWrapperClass:String,arrowWrapperStyle:[String,Object],displayDirective:String,x:Number,y:Number,flip:Boolean,overlap:Boolean,placement:String,width:[Number,String],keepAliveOnHover:Boolean,scrollable:Boolean,contentClass:String,contentStyle:[Object,String],headerClass:String,headerStyle:[Object,String],footerClass:String,footerStyle:[Object,String],internalDeactivateImmediately:Boolean,animated:Boolean,onClickoutside:Function,internalTrapFocus:Boolean,internalOnAfterLeave:Function,minWidth:Number,maxWidth:Number});function _i({arrowClass:e,arrowStyle:t,arrowWrapperClass:n,arrowWrapperStyle:r,clsPrefix:o}){return y("div",{key:"__popover-arrow__",style:r,class:[`${o}-popover-arrow-wrapper`,n]},y("div",{class:[`${o}-popover-arrow`,e],style:t}))}const Ei=G({name:"PopoverBody",inheritAttrs:!1,props:tr,setup(e,{slots:t,attrs:n}){const{namespaceRef:r,mergedClsPrefixRef:o,inlineThemeDisabled:a}=Ue(e),s=de("Popover","-popover",zi,Qr,e,o),l=M(null),i=Q("NPopover"),h=M(null),g=M(e.show),w=M(!1);At(()=>{const{show:S}=e;S&&!Qo()&&!e.internalDeactivateImmediately&&(w.value=!0)});const u=K(()=>{const{trigger:S,onClickoutside:A}=e,F=[],{positionManuallyRef:{value:E}}=i;return E||(S==="click"&&!A&&F.push([Qt,B,void 0,{capture:!0}]),S==="hover"&&F.push([Fo,L])),A&&F.push([Qt,B,void 0,{capture:!0}]),(e.displayDirective==="show"||e.animated&&w.value)&&F.push([An,e.show]),F}),d=K(()=>{const{common:{cubicBezierEaseInOut:S,cubicBezierEaseIn:A,cubicBezierEaseOut:F},self:{space:E,spaceArrow:V,padding:Y,fontSize:N,textColor:q,dividerColor:C,color:R,boxShadow:X,borderRadius:ae,arrowHeight:te,arrowOffset:Z,arrowOffsetVertical:Le}}=s.value;return{"--n-box-shadow":X,"--n-bezier":S,"--n-bezier-ease-in":A,"--n-bezier-ease-out":F,"--n-font-size":N,"--n-text-color":q,"--n-color":R,"--n-divider-color":C,"--n-border-radius":ae,"--n-arrow-height":te,"--n-arrow-offset":Z,"--n-arrow-offset-vertical":Le,"--n-padding":Y,"--n-space":E,"--n-space-arrow":V}}),x=K(()=>{const S=e.width==="trigger"?void 0:ct(e.width),A=[];S&&A.push({width:S});const{maxWidth:F,minWidth:E}=e;return F&&A.push({maxWidth:ct(F)}),E&&A.push({maxWidth:ct(E)}),a||A.push(d.value),A}),b=a?ot("popover",void 0,d,e):void 0;i.setBodyInstance({syncPosition:v}),Oe(()=>{i.setBodyInstance(null)}),ie(D(e,"show"),S=>{e.animated||(S?g.value=!0:g.value=!1)});function v(){var S;(S=l.value)===null||S===void 0||S.syncPosition()}function c(S){e.trigger==="hover"&&e.keepAliveOnHover&&e.show&&i.handleMouseEnter(S)}function T(S){e.trigger==="hover"&&e.keepAliveOnHover&&i.handleMouseLeave(S)}function L(S){e.trigger==="hover"&&!_().contains(Ct(S))&&i.handleMouseMoveOutside(S)}function B(S){(e.trigger==="click"&&!_().contains(Ct(S))||e.onClickoutside)&&i.handleClickOutside(S)}function _(){return i.getTriggerElement()}ve(Dn,h),ve(Rn,null),ve(Hn,null);function z(){if(b==null||b.onRender(),!(e.displayDirective==="show"||e.show||e.animated&&w.value))return null;let A;const F=i.internalRenderBodyRef.value,{value:E}=o;if(F)A=F([`${E}-popover-shared`,b==null?void 0:b.themeClass.value,e.overlap&&`${E}-popover-shared--overlap`,e.showArrow&&`${E}-popover-shared--show-arrow`,e.arrowPointToCenter&&`${E}-popover-shared--center-arrow`],h,x.value,c,T);else{const{value:V}=i.extraClassRef,{internalTrapFocus:Y}=e,N=!Ut(t.header)||!Ut(t.footer),q=()=>{var C,R;const X=N?y(at,null,be(t.header,Z=>Z?y("div",{class:[`${E}-popover__header`,e.headerClass],style:e.headerStyle},Z):null),be(t.default,Z=>Z?y("div",{class:[`${E}-popover__content`,e.contentClass],style:e.contentStyle},t):null),be(t.footer,Z=>Z?y("div",{class:[`${E}-popover__footer`,e.footerClass],style:e.footerStyle},Z):null)):e.scrollable?(C=t.default)===null||C===void 0?void 0:C.call(t):y("div",{class:[`${E}-popover__content`,e.contentClass],style:e.contentStyle},t),ae=e.scrollable?y(eo,{contentClass:N?void 0:`${E}-popover__content ${(R=e.contentClass)!==null&&R!==void 0?R:""}`,contentStyle:N?void 0:e.contentStyle},{default:()=>X}):X,te=e.showArrow?_i({arrowClass:e.arrowClass,arrowStyle:e.arrowStyle,arrowWrapperClass:e.arrowWrapperClass,arrowWrapperStyle:e.arrowWrapperStyle,clsPrefix:E}):null;return[ae,te]};A=y("div",On({class:[`${E}-popover`,`${E}-popover-shared`,b==null?void 0:b.themeClass.value,V.map(C=>`${E}-${C}`),{[`${E}-popover--scrollable`]:e.scrollable,[`${E}-popover--show-header-or-footer`]:N,[`${E}-popover--raw`]:e.raw,[`${E}-popover-shared--overlap`]:e.overlap,[`${E}-popover-shared--show-arrow`]:e.showArrow,[`${E}-popover-shared--center-arrow`]:e.arrowPointToCenter}],ref:h,style:x.value,onKeydown:i.handleKeydown,onMouseenter:c,onMouseleave:T},n),Y?y(Jo,{active:e.show,autoFocus:!0},{default:q}):q())}return Ve(A,u.value)}return{displayed:w,namespace:r,isMounted:i.isMountedRef,zIndex:i.zIndexRef,followerRef:l,adjustedTo:Be(e),followerEnabled:g,renderContentNode:z}},render(){return y(Yo,{ref:"followerRef",zIndex:this.zIndex,show:this.show,enabled:this.followerEnabled,to:this.adjustedTo,x:this.x,y:this.y,flip:this.flip,placement:this.placement,containerClass:this.namespace,overlap:this.overlap,width:this.width==="trigger"?"target":void 0,teleportDisabled:this.adjustedTo===Be.tdkey},{default:()=>this.animated?y(vo,{name:"popover-transition",appear:this.isMounted,onEnter:()=>{this.followerEnabled=!0},onAfterLeave:()=>{var e;(e=this.internalOnAfterLeave)===null||e===void 0||e.call(this),this.followerEnabled=!1,this.displayed=!1}},{default:this.renderContentNode}):this.renderContentNode()})}}),ki=Object.keys(tr),Ii={focus:["onFocus","onBlur"],click:["onClick"],hover:["onMouseenter","onMouseleave"],manual:[],nested:["onFocus","onBlur","onMouseenter","onMouseleave","onClick"]};function Bi(e,t,n){Ii[t].forEach(r=>{e.props?e.props=Object.assign({},e.props):e.props={};const o=e.props[r],a=n[r];o?e.props[r]=(...s)=>{o(...s),a(...s)}:e.props[r]=a})}const nr={show:{type:Boolean,default:void 0},defaultShow:Boolean,showArrow:{type:Boolean,default:!0},trigger:{type:String,default:"hover"},delay:{type:Number,default:100},duration:{type:Number,default:100},raw:Boolean,placement:{type:String,default:"top"},x:Number,y:Number,arrowPointToCenter:Boolean,disabled:Boolean,getDisabled:Function,displayDirective:{type:String,default:"if"},arrowClass:String,arrowStyle:[String,Object],arrowWrapperClass:String,arrowWrapperStyle:[String,Object],flip:{type:Boolean,default:!0},animated:{type:Boolean,default:!0},width:{type:[Number,String],default:void 0},overlap:Boolean,keepAliveOnHover:{type:Boolean,default:!0},zIndex:Number,to:Be.propTo,scrollable:Boolean,contentClass:String,contentStyle:[Object,String],headerClass:String,headerStyle:[Object,String],footerClass:String,footerStyle:[Object,String],onClickoutside:Function,"onUpdate:show":[Function,Array],onUpdateShow:[Function,Array],internalDeactivateImmediately:Boolean,internalSyncTargetWithParent:Boolean,internalInheritedEventHandlers:{type:Array,default:()=>[]},internalTrapFocus:Boolean,internalExtraClass:{type:Array,default:()=>[]},onShow:[Function,Array],onHide:[Function,Array],arrow:{type:Boolean,default:void 0},minWidth:Number,maxWidth:Number},Mi=Object.assign(Object.assign(Object.assign({},de.props),nr),{internalOnAfterLeave:Function,internalRenderBody:Function}),Ai=G({name:"Popover",inheritAttrs:!1,props:Mi,__popover__:!0,setup(e){const t=Tn(),n=M(null),r=K(()=>e.show),o=M(e.defaultShow),a=_n(r,o),s=De(()=>e.disabled?!1:a.value),l=()=>{if(e.disabled)return!0;const{getDisabled:C}=e;return!!(C!=null&&C())},i=()=>l()?!1:a.value,h=$t(e,["arrow","showArrow"]),g=K(()=>e.overlap?!1:h.value);let w=null;const u=M(null),d=M(null),x=De(()=>e.x!==void 0&&e.y!==void 0);function b(C){const{"onUpdate:show":R,onUpdateShow:X,onShow:ae,onHide:te}=e;o.value=C,R&&oe(R,C),X&&oe(X,C),C&&ae&&oe(ae,!0),C&&te&&oe(te,!1)}function v(){w&&w.syncPosition()}function c(){const{value:C}=u;C&&(window.clearTimeout(C),u.value=null)}function T(){const{value:C}=d;C&&(window.clearTimeout(C),d.value=null)}function L(){const C=l();if(e.trigger==="focus"&&!C){if(i())return;b(!0)}}function B(){const C=l();if(e.trigger==="focus"&&!C){if(!i())return;b(!1)}}function _(){const C=l();if(e.trigger==="hover"&&!C){if(T(),u.value!==null||i())return;const R=()=>{b(!0),u.value=null},{delay:X}=e;X===0?R():u.value=window.setTimeout(R,X)}}function z(){const C=l();if(e.trigger==="hover"&&!C){if(c(),d.value!==null||!i())return;const R=()=>{b(!1),d.value=null},{duration:X}=e;X===0?R():d.value=window.setTimeout(R,X)}}function S(){z()}function A(C){var R;i()&&(e.trigger==="click"&&(c(),T(),b(!1)),(R=e.onClickoutside)===null||R===void 0||R.call(e,C))}function F(){if(e.trigger==="click"&&!l()){c(),T();const C=!i();b(C)}}function E(C){e.internalTrapFocus&&C.key==="Escape"&&(c(),T(),b(!1))}function V(C){o.value=C}function Y(){var C;return(C=n.value)===null||C===void 0?void 0:C.targetRef}function N(C){w=C}return ve("NPopover",{getTriggerElement:Y,handleKeydown:E,handleMouseEnter:_,handleMouseLeave:z,handleClickOutside:A,handleMouseMoveOutside:S,setBodyInstance:N,positionManuallyRef:x,isMountedRef:t,zIndexRef:D(e,"zIndex"),extraClassRef:D(e,"internalExtraClass"),internalRenderBodyRef:D(e,"internalRenderBody")}),At(()=>{a.value&&l()&&b(!1)}),{binderInstRef:n,positionManually:x,mergedShowConsideringDisabledProp:s,uncontrolledShow:o,mergedShowArrow:g,getMergedShow:i,setShow:V,handleClick:F,handleMouseEnter:_,handleMouseLeave:z,handleFocus:L,handleBlur:B,syncPosition:v}},render(){var e;const{positionManually:t,$slots:n}=this;let r,o=!1;if(!t&&(n.activator?r=on(n,"activator"):r=on(n,"trigger"),r)){r=Ln(r),r=r.type===go?y("span",[r]):r;const a={onClick:this.handleClick,onMouseenter:this.handleMouseEnter,onMouseleave:this.handleMouseLeave,onFocus:this.handleFocus,onBlur:this.handleBlur};if(!((e=r.type)===null||e===void 0)&&e.__popover__)o=!0,r.props||(r.props={internalSyncTargetWithParent:!0,internalInheritedEventHandlers:[]}),r.props.internalSyncTargetWithParent=!0,r.props.internalInheritedEventHandlers?r.props.internalInheritedEventHandlers=[a,...r.props.internalInheritedEventHandlers]:r.props.internalInheritedEventHandlers=[a];else{const{internalInheritedEventHandlers:s}=this,l=[a,...s],i={onBlur:h=>{l.forEach(g=>{g.onBlur(h)})},onFocus:h=>{l.forEach(g=>{g.onFocus(h)})},onClick:h=>{l.forEach(g=>{g.onClick(h)})},onMouseenter:h=>{l.forEach(g=>{g.onMouseenter(h)})},onMouseleave:h=>{l.forEach(g=>{g.onMouseleave(h)})}};Bi(r,s?"nested":t?"manual":this.trigger,i)}}return y(Oo,{ref:"binderInstRef",syncTarget:!o,syncTargetWithParent:this.internalSyncTargetWithParent},{default:()=>{this.mergedShowConsideringDisabledProp;const a=this.getMergedShow();return[this.internalTrapFocus&&a?Ve(y("div",{style:{position:"fixed",top:0,right:0,bottom:0,left:0}}),[[Vn,{enabled:a,zIndex:this.zIndex}]]):null,t?null:y(Lo,null,{default:()=>r}),y(Ei,qn(this.$props,ki,Object.assign(Object.assign({},this.$attrs),{showArrow:this.mergedShowArrow,show:a})),{default:()=>{var s,l;return(l=(s=this.$slots).default)===null||l===void 0?void 0:l.call(s)},header:()=>{var s,l;return(l=(s=this.$slots).header)===null||l===void 0?void 0:l.call(s)},footer:()=>{var s,l;return(l=(s=this.$slots).footer)===null||l===void 0?void 0:l.call(s)}})]}})}});function Oi(e){const{textColor2:t,primaryColorHover:n,primaryColorPressed:r,primaryColor:o,infoColor:a,successColor:s,warningColor:l,errorColor:i,baseColor:h,borderColor:g,opacityDisabled:w,tagColor:u,closeIconColor:d,closeIconColorHover:x,closeIconColorPressed:b,borderRadiusSmall:v,fontSizeMini:c,fontSizeTiny:T,fontSizeSmall:L,fontSizeMedium:B,heightMini:_,heightTiny:z,heightSmall:S,heightMedium:A,closeColorHover:F,closeColorPressed:E,buttonColor2Hover:V,buttonColor2Pressed:Y,fontWeightStrong:N}=e;return Object.assign(Object.assign({},no),{closeBorderRadius:v,heightTiny:_,heightSmall:z,heightMedium:S,heightLarge:A,borderRadius:v,opacityDisabled:w,fontSizeTiny:c,fontSizeSmall:T,fontSizeMedium:L,fontSizeLarge:B,fontWeightStrong:N,textColorCheckable:t,textColorHoverCheckable:t,textColorPressedCheckable:t,textColorChecked:h,colorCheckable:"#0000",colorHoverCheckable:V,colorPressedCheckable:Y,colorChecked:o,colorCheckedHover:n,colorCheckedPressed:r,border:`1px solid ${g}`,textColor:t,color:u,colorBordered:"rgb(250, 250, 252)",closeIconColor:d,closeIconColorHover:x,closeIconColorPressed:b,closeColorHover:F,closeColorPressed:E,borderPrimary:`1px solid ${W(o,{alpha:.3})}`,textColorPrimary:o,colorPrimary:W(o,{alpha:.12}),colorBorderedPrimary:W(o,{alpha:.1}),closeIconColorPrimary:o,closeIconColorHoverPrimary:o,closeIconColorPressedPrimary:o,closeColorHoverPrimary:W(o,{alpha:.12}),closeColorPressedPrimary:W(o,{alpha:.18}),borderInfo:`1px solid ${W(a,{alpha:.3})}`,textColorInfo:a,colorInfo:W(a,{alpha:.12}),colorBorderedInfo:W(a,{alpha:.1}),closeIconColorInfo:a,closeIconColorHoverInfo:a,closeIconColorPressedInfo:a,closeColorHoverInfo:W(a,{alpha:.12}),closeColorPressedInfo:W(a,{alpha:.18}),borderSuccess:`1px solid ${W(s,{alpha:.3})}`,textColorSuccess:s,colorSuccess:W(s,{alpha:.12}),colorBorderedSuccess:W(s,{alpha:.1}),closeIconColorSuccess:s,closeIconColorHoverSuccess:s,closeIconColorPressedSuccess:s,closeColorHoverSuccess:W(s,{alpha:.12}),closeColorPressedSuccess:W(s,{alpha:.18}),borderWarning:`1px solid ${W(l,{alpha:.35})}`,textColorWarning:l,colorWarning:W(l,{alpha:.15}),colorBorderedWarning:W(l,{alpha:.12}),closeIconColorWarning:l,closeIconColorHoverWarning:l,closeIconColorPressedWarning:l,closeColorHoverWarning:W(l,{alpha:.12}),closeColorPressedWarning:W(l,{alpha:.18}),borderError:`1px solid ${W(i,{alpha:.23})}`,textColorError:i,colorError:W(i,{alpha:.1}),colorBorderedError:W(i,{alpha:.08}),closeIconColorError:i,closeIconColorHoverError:i,closeIconColorPressedError:i,closeColorHoverError:W(i,{alpha:.12}),closeColorPressedError:W(i,{alpha:.18})})}const Li={common:to,self:Oi},Fi={color:Object,type:{type:String,default:"default"},round:Boolean,size:{type:String,default:"medium"},closable:Boolean,disabled:{type:Boolean,default:void 0}},Wi=m("tag",`
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
 `),O("border",`
 pointer-events: none;
 position: absolute;
 left: 0;
 right: 0;
 top: 0;
 bottom: 0;
 border-radius: inherit;
 border: var(--n-border);
 transition: border-color .3s var(--n-bezier);
 `),O("icon",`
 display: flex;
 margin: 0 4px 0 0;
 color: var(--n-text-color);
 transition: color .3s var(--n-bezier);
 font-size: var(--n-avatar-size-override);
 `),O("avatar",`
 display: flex;
 margin: 0 6px 0 0;
 `),O("close",`
 margin: var(--n-close-margin);
 transition:
 background-color .3s var(--n-bezier),
 color .3s var(--n-bezier);
 `),$("round",`
 padding: 0 calc(var(--n-height) / 3);
 border-radius: calc(var(--n-height) / 2);
 `,[O("icon",`
 margin: 0 4px 0 calc((var(--n-height) - 8px) / -2);
 `),O("avatar",`
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
 `,[pe("disabled",[I("&:hover","background-color: var(--n-color-hover-checkable);",[pe("checked","color: var(--n-text-color-hover-checkable);")]),I("&:active","background-color: var(--n-color-pressed-checkable);",[pe("checked","color: var(--n-text-color-pressed-checkable);")])]),$("checked",`
 color: var(--n-text-color-checked);
 background-color: var(--n-color-checked);
 `,[pe("disabled",[I("&:hover","background-color: var(--n-color-checked-hover);"),I("&:active","background-color: var(--n-color-checked-pressed);")])])])]),Ri=Object.assign(Object.assign(Object.assign({},de.props),Fi),{bordered:{type:Boolean,default:void 0},checked:Boolean,checkable:Boolean,strong:Boolean,triggerClickOnClose:Boolean,onClose:[Array,Function],onMouseenter:Function,onMouseleave:Function,"onUpdate:checked":Function,onUpdateChecked:Function,internalCloseFocusable:{type:Boolean,default:!0},internalCloseIsButtonTag:{type:Boolean,default:!0},onCheckedChange:Function}),Hi=ge("n-tag"),us=G({name:"Tag",props:Ri,setup(e){const t=M(null),{mergedBorderedRef:n,mergedClsPrefixRef:r,inlineThemeDisabled:o,mergedRtlRef:a}=Ue(e),s=de("Tag","-tag",Wi,Li,e,r);ve(Hi,{roundRef:D(e,"round")});function l(){if(!e.disabled&&e.checkable){const{checked:d,onCheckedChange:x,onUpdateChecked:b,"onUpdate:checked":v}=e;b&&b(!d),v&&v(!d),x&&x(!d)}}function i(d){if(e.triggerClickOnClose||d.stopPropagation(),!e.disabled){const{onClose:x}=e;x&&oe(x,d)}}const h={setTextContent(d){const{value:x}=t;x&&(x.textContent=d)}},g=ro("Tag",a,r),w=K(()=>{const{type:d,size:x,color:{color:b,textColor:v}={}}=e,{common:{cubicBezierEaseInOut:c},self:{padding:T,closeMargin:L,borderRadius:B,opacityDisabled:_,textColorCheckable:z,textColorHoverCheckable:S,textColorPressedCheckable:A,textColorChecked:F,colorCheckable:E,colorHoverCheckable:V,colorPressedCheckable:Y,colorChecked:N,colorCheckedHover:q,colorCheckedPressed:C,closeBorderRadius:R,fontWeightStrong:X,[H("colorBordered",d)]:ae,[H("closeSize",x)]:te,[H("closeIconSize",x)]:Z,[H("fontSize",x)]:Le,[H("height",x)]:Xe,[H("color",d)]:it,[H("textColor",d)]:Ke,[H("border",d)]:ce,[H("closeIconColor",d)]:xe,[H("closeIconColorHover",d)]:Ye,[H("closeIconColorPressed",d)]:st,[H("closeColorHover",d)]:lt,[H("closeColorPressed",d)]:ue}}=s.value,we=Re(L);return{"--n-font-weight-strong":X,"--n-avatar-size-override":`calc(${Xe} - 8px)`,"--n-bezier":c,"--n-border-radius":B,"--n-border":ce,"--n-close-icon-size":Z,"--n-close-color-pressed":ue,"--n-close-color-hover":lt,"--n-close-border-radius":R,"--n-close-icon-color":xe,"--n-close-icon-color-hover":Ye,"--n-close-icon-color-pressed":st,"--n-close-icon-color-disabled":xe,"--n-close-margin-top":we.top,"--n-close-margin-right":we.right,"--n-close-margin-bottom":we.bottom,"--n-close-margin-left":we.left,"--n-close-size":te,"--n-color":b||(n.value?ae:it),"--n-color-checkable":E,"--n-color-checked":N,"--n-color-checked-hover":q,"--n-color-checked-pressed":C,"--n-color-hover-checkable":V,"--n-color-pressed-checkable":Y,"--n-font-size":Le,"--n-height":Xe,"--n-opacity-disabled":_,"--n-padding":T,"--n-text-color":v||Ke,"--n-text-color-checkable":z,"--n-text-color-checked":F,"--n-text-color-hover-checkable":S,"--n-text-color-pressed-checkable":A}}),u=o?ot("tag",K(()=>{let d="";const{type:x,size:b,color:{color:v,textColor:c}={}}=e;return d+=x[0],d+=b[0],v&&(d+=`a${Vt(v)}`),c&&(d+=`b${Vt(c)}`),n.value&&(d+="c"),d}),w,e):void 0;return Object.assign(Object.assign({},h),{rtlEnabled:g,mergedClsPrefix:r,contentRef:t,mergedBordered:n,handleClick:l,handleCloseClick:i,cssVars:o?void 0:w,themeClass:u==null?void 0:u.themeClass,onRender:u==null?void 0:u.onRender})},render(){var e,t;const{mergedClsPrefix:n,rtlEnabled:r,closable:o,color:{borderColor:a}={},round:s,onRender:l,$slots:i}=this;l==null||l();const h=be(i.avatar,w=>w&&y("div",{class:`${n}-tag__avatar`},w)),g=be(i.icon,w=>w&&y("div",{class:`${n}-tag__icon`},w));return y("div",{class:[`${n}-tag`,this.themeClass,{[`${n}-tag--rtl`]:r,[`${n}-tag--strong`]:this.strong,[`${n}-tag--disabled`]:this.disabled,[`${n}-tag--checkable`]:this.checkable,[`${n}-tag--checked`]:this.checkable&&this.checked,[`${n}-tag--round`]:s,[`${n}-tag--avatar`]:h,[`${n}-tag--icon`]:g,[`${n}-tag--closable`]:o}],style:this.cssVars,onClick:this.handleClick,onMouseenter:this.onMouseenter,onMouseleave:this.onMouseleave},g||h,y("span",{class:`${n}-tag__content`,ref:"contentRef"},(t=(e=this.$slots).default)===null||t===void 0?void 0:t.call(e)),!this.checkable&&o?y(En,{clsPrefix:n,class:`${n}-tag__close`,disabled:this.disabled,onClick:this.handleCloseClick,focusable:this.internalCloseFocusable,round:s,isButtonTag:this.internalCloseIsButtonTag,absolute:!0}):null,!this.checkable&&this.mergedBordered?y("div",{class:`${n}-tag__border`,style:{borderColor:a}}):null)}});function fs(){const e=Q(oo,null);return e===null&&kn("use-message","No outer <n-message-provider /> founded. See prerequisite in https://www.naiveui.com/en-US/os-theme/components/message for more details. If you want to use `useMessage` outside setup, please check https://www.naiveui.com/zh-CN/os-theme/components/message#Q-&-A."),e}const rr=ge("n-popconfirm"),or={positiveText:String,negativeText:String,showIcon:{type:Boolean,default:!0},onPositiveClick:{type:Function,required:!0},onNegativeClick:{type:Function,required:!0}},yn=io(or),Di=G({name:"NPopconfirmPanel",props:or,setup(e){const{localeRef:t}=Yt("Popconfirm"),{inlineThemeDisabled:n}=Ue(),{mergedClsPrefixRef:r,mergedThemeRef:o,props:a}=Q(rr),s=K(()=>{const{common:{cubicBezierEaseInOut:i},self:{fontSize:h,iconSize:g,iconColor:w}}=o.value;return{"--n-bezier":i,"--n-font-size":h,"--n-icon-size":g,"--n-icon-color":w}}),l=n?ot("popconfirm-panel",void 0,s,a):void 0;return Object.assign(Object.assign({},Yt("Popconfirm")),{mergedClsPrefix:r,cssVars:n?void 0:s,localizedPositiveText:K(()=>e.positiveText||t.value.positiveText),localizedNegativeText:K(()=>e.negativeText||t.value.negativeText),positiveButtonProps:D(a,"positiveButtonProps"),negativeButtonProps:D(a,"negativeButtonProps"),handlePositiveClick(i){e.onPositiveClick(i)},handleNegativeClick(i){e.onNegativeClick(i)},themeClass:l==null?void 0:l.themeClass,onRender:l==null?void 0:l.onRender})},render(){var e;const{mergedClsPrefix:t,showIcon:n,$slots:r}=this,o=Xt(r.action,()=>this.negativeText===null&&this.positiveText===null?[]:[this.negativeText!==null&&y(Kt,Object.assign({size:"small",onClick:this.handleNegativeClick},this.negativeButtonProps),{default:()=>this.localizedNegativeText}),this.positiveText!==null&&y(Kt,Object.assign({size:"small",type:"primary",onClick:this.handlePositiveClick},this.positiveButtonProps),{default:()=>this.localizedPositiveText})]);return(e=this.onRender)===null||e===void 0||e.call(this),y("div",{class:[`${t}-popconfirm__panel`,this.themeClass],style:this.cssVars},be(r.default,a=>n||a?y("div",{class:`${t}-popconfirm__body`},n?y("div",{class:`${t}-popconfirm__icon`},Xt(r.icon,()=>[y(In,{clsPrefix:t},{default:()=>y(ao,null)})])):null,a):null),o?y("div",{class:[`${t}-popconfirm__action`]},o):null)}}),Ni=m("popconfirm",[O("body",`
 font-size: var(--n-font-size);
 display: flex;
 align-items: center;
 flex-wrap: nowrap;
 position: relative;
 `,[O("icon",`
 display: flex;
 font-size: var(--n-icon-size);
 color: var(--n-icon-color);
 transition: color .3s var(--n-bezier);
 margin: 0 8px 0 0;
 `)]),O("action",`
 display: flex;
 justify-content: flex-end;
 `,[I("&:not(:first-child)","margin-top: 8px"),m("button",[I("&:not(:last-child)","margin-right: 8px;")])])]),ji=Object.assign(Object.assign(Object.assign({},de.props),nr),{positiveText:String,negativeText:String,showIcon:{type:Boolean,default:!0},trigger:{type:String,default:"click"},positiveButtonProps:Object,negativeButtonProps:Object,onPositiveClick:Function,onNegativeClick:Function}),hs=G({name:"Popconfirm",props:ji,__popover__:!0,setup(e){const{mergedClsPrefixRef:t}=Ue(),n=de("Popconfirm","-popconfirm",Ni,so,e,t),r=M(null);function o(l){var i;if(!(!((i=r.value)===null||i===void 0)&&i.getMergedShow()))return;const{onPositiveClick:h,"onUpdate:show":g}=e;Promise.resolve(h?h(l):!0).then(w=>{var u;w!==!1&&((u=r.value)===null||u===void 0||u.setShow(!1),g&&oe(g,!1))})}function a(l){var i;if(!(!((i=r.value)===null||i===void 0)&&i.getMergedShow()))return;const{onNegativeClick:h,"onUpdate:show":g}=e;Promise.resolve(h?h(l):!0).then(w=>{var u;w!==!1&&((u=r.value)===null||u===void 0||u.setShow(!1),g&&oe(g,!1))})}return ve(rr,{mergedThemeRef:n,mergedClsPrefixRef:t,props:e}),{setShow(l){var i;(i=r.value)===null||i===void 0||i.setShow(l)},syncPosition(){var l;(l=r.value)===null||l===void 0||l.syncPosition()},mergedTheme:n,popoverInstRef:r,handlePositiveClick:o,handleNegativeClick:a}},render(){const{$slots:e,$props:t,mergedTheme:n}=this;return y(Ai,Bn(t,yn,{theme:n.peers.Popover,themeOverrides:n.peerOverrides.Popover,internalExtraClass:["popconfirm"],ref:"popoverInstRef"}),{trigger:e.activator||e.trigger,default:()=>{const r=qn(t,yn);return y(Di,Object.assign(Object.assign({},r),{onPositiveClick:this.handlePositiveClick,onNegativeClick:this.handleNegativeClick}),e)}})}}),Ft=ge("n-tabs"),ar={tab:[String,Number,Object,Function],name:{type:[String,Number],required:!0},disabled:Boolean,displayDirective:{type:String,default:"if"},closable:{type:Boolean,default:void 0},tabProps:Object,label:[String,Number,Object,Function]},ps=G({__TAB_PANE__:!0,name:"TabPane",alias:["TabPanel"],props:ar,setup(e){const t=Q(Ft,null);return t||kn("tab-pane","`n-tab-pane` must be placed inside `n-tabs`."),{style:t.paneStyleRef,class:t.paneClassRef,mergedClsPrefix:t.mergedClsPrefixRef}},render(){return y("div",{class:[`${this.mergedClsPrefix}-tab-pane`,this.class],style:this.style},this.$slots)}}),Ui=Object.assign({internalLeftPadded:Boolean,internalAddable:Boolean,internalCreatedByPane:Boolean},Bn(ar,["displayDirective"])),It=G({__TAB__:!0,inheritAttrs:!1,name:"Tab",props:Ui,setup(e){const{mergedClsPrefixRef:t,valueRef:n,typeRef:r,closableRef:o,tabStyleRef:a,addTabStyleRef:s,tabClassRef:l,addTabClassRef:i,tabChangeIdRef:h,onBeforeLeaveRef:g,triggerRef:w,handleAdd:u,activateTab:d,handleClose:x}=Q(Ft);return{trigger:w,mergedClosable:K(()=>{if(e.internalAddable)return!1;const{closable:b}=e;return b===void 0?o.value:b}),style:a,addStyle:s,tabClass:l,addTabClass:i,clsPrefix:t,value:n,type:r,handleClose(b){b.stopPropagation(),!e.disabled&&x(e.name)},activateTab(){if(e.disabled)return;if(e.internalAddable){u();return}const{name:b}=e,v=++h.id;if(b!==n.value){const{value:c}=g;c?Promise.resolve(c(e.name,n.value)).then(T=>{T&&h.id===v&&d(b)}):d(b)}}}},render(){const{internalAddable:e,clsPrefix:t,name:n,disabled:r,label:o,tab:a,value:s,mergedClosable:l,trigger:i,$slots:{default:h}}=this,g=o??a;return y("div",{class:`${t}-tabs-tab-wrapper`},this.internalLeftPadded?y("div",{class:`${t}-tabs-tab-pad`}):null,y("div",Object.assign({key:n,"data-name":n,"data-disabled":r?!0:void 0},On({class:[`${t}-tabs-tab`,s===n&&`${t}-tabs-tab--active`,r&&`${t}-tabs-tab--disabled`,l&&`${t}-tabs-tab--closable`,e&&`${t}-tabs-tab--addable`,e?this.addTabClass:this.tabClass],onClick:i==="click"?this.activateTab:void 0,onMouseenter:i==="hover"?this.activateTab:void 0,style:e?this.addStyle:this.style},this.internalCreatedByPane?this.tabProps||{}:this.$attrs)),y("span",{class:`${t}-tabs-tab__label`},e?y(at,null,y("div",{class:`${t}-tabs-tab__height-placeholder`}," "),y(In,{clsPrefix:t},{default:()=>y(Pi,null)})):h?h():typeof g=="object"?g:lo(g??n)),l&&this.type==="card"?y(En,{clsPrefix:t,class:`${t}-tabs-tab__close`,onClick:this.handleClose,disabled:r}):null))}}),Vi=m("tabs",`
 box-sizing: border-box;
 width: 100%;
 display: flex;
 flex-direction: column;
 transition:
 background-color .3s var(--n-bezier),
 border-color .3s var(--n-bezier);
`,[$("segment-type",[m("tabs-rail",[I("&.transition-disabled",[m("tabs-capsule",`
 transition: none;
 `)])])]),$("top",[m("tab-pane",`
 padding: var(--n-pane-padding-top) var(--n-pane-padding-right) var(--n-pane-padding-bottom) var(--n-pane-padding-left);
 `)]),$("left",[m("tab-pane",`
 padding: var(--n-pane-padding-right) var(--n-pane-padding-bottom) var(--n-pane-padding-left) var(--n-pane-padding-top);
 `)]),$("left, right",`
 flex-direction: row;
 `,[m("tabs-bar",`
 width: 2px;
 right: 0;
 transition:
 top .2s var(--n-bezier),
 max-height .2s var(--n-bezier),
 background-color .3s var(--n-bezier);
 `),m("tabs-tab",`
 padding: var(--n-tab-padding-vertical); 
 `)]),$("right",`
 flex-direction: row-reverse;
 `,[m("tab-pane",`
 padding: var(--n-pane-padding-left) var(--n-pane-padding-top) var(--n-pane-padding-right) var(--n-pane-padding-bottom);
 `),m("tabs-bar",`
 left: 0;
 `)]),$("bottom",`
 flex-direction: column-reverse;
 justify-content: flex-end;
 `,[m("tab-pane",`
 padding: var(--n-pane-padding-bottom) var(--n-pane-padding-right) var(--n-pane-padding-top) var(--n-pane-padding-left);
 `),m("tabs-bar",`
 top: 0;
 `)]),m("tabs-rail",`
 position: relative;
 padding: 3px;
 border-radius: var(--n-tab-border-radius);
 width: 100%;
 background-color: var(--n-color-segment);
 transition: background-color .3s var(--n-bezier);
 display: flex;
 align-items: center;
 `,[m("tabs-capsule",`
 border-radius: var(--n-tab-border-radius);
 position: absolute;
 pointer-events: none;
 background-color: var(--n-tab-color-segment);
 box-shadow: 0 1px 3px 0 rgba(0, 0, 0, .08);
 transition: transform 0.3s var(--n-bezier);
 `),m("tabs-tab-wrapper",`
 flex-basis: 0;
 flex-grow: 1;
 display: flex;
 align-items: center;
 justify-content: center;
 `,[m("tabs-tab",`
 overflow: hidden;
 border-radius: var(--n-tab-border-radius);
 width: 100%;
 display: flex;
 align-items: center;
 justify-content: center;
 `,[$("active",`
 font-weight: var(--n-font-weight-strong);
 color: var(--n-tab-text-color-active);
 `),I("&:hover",`
 color: var(--n-tab-text-color-hover);
 `)])])]),$("flex",[m("tabs-nav",`
 width: 100%;
 position: relative;
 `,[m("tabs-wrapper",`
 width: 100%;
 `,[m("tabs-tab",`
 margin-right: 0;
 `)])])]),m("tabs-nav",`
 box-sizing: border-box;
 line-height: 1.5;
 display: flex;
 transition: border-color .3s var(--n-bezier);
 `,[O("prefix, suffix",`
 display: flex;
 align-items: center;
 `),O("prefix","padding-right: 16px;"),O("suffix","padding-left: 16px;")]),$("top, bottom",[m("tabs-nav-scroll-wrapper",[I("&::before",`
 top: 0;
 bottom: 0;
 left: 0;
 width: 20px;
 `),I("&::after",`
 top: 0;
 bottom: 0;
 right: 0;
 width: 20px;
 `),$("shadow-start",[I("&::before",`
 box-shadow: inset 10px 0 8px -8px rgba(0, 0, 0, .12);
 `)]),$("shadow-end",[I("&::after",`
 box-shadow: inset -10px 0 8px -8px rgba(0, 0, 0, .12);
 `)])])]),$("left, right",[m("tabs-nav-scroll-content",`
 flex-direction: column;
 `),m("tabs-nav-scroll-wrapper",[I("&::before",`
 top: 0;
 left: 0;
 right: 0;
 height: 20px;
 `),I("&::after",`
 bottom: 0;
 left: 0;
 right: 0;
 height: 20px;
 `),$("shadow-start",[I("&::before",`
 box-shadow: inset 0 10px 8px -8px rgba(0, 0, 0, .12);
 `)]),$("shadow-end",[I("&::after",`
 box-shadow: inset 0 -10px 8px -8px rgba(0, 0, 0, .12);
 `)])])]),m("tabs-nav-scroll-wrapper",`
 flex: 1;
 position: relative;
 overflow: hidden;
 `,[m("tabs-nav-y-scroll",`
 height: 100%;
 width: 100%;
 overflow-y: auto; 
 scrollbar-width: none;
 `,[I("&::-webkit-scrollbar, &::-webkit-scrollbar-track-piece, &::-webkit-scrollbar-thumb",`
 width: 0;
 height: 0;
 display: none;
 `)]),I("&::before, &::after",`
 transition: box-shadow .3s var(--n-bezier);
 pointer-events: none;
 content: "";
 position: absolute;
 z-index: 1;
 `)]),m("tabs-nav-scroll-content",`
 display: flex;
 position: relative;
 min-width: 100%;
 min-height: 100%;
 width: fit-content;
 box-sizing: border-box;
 `),m("tabs-wrapper",`
 display: inline-flex;
 flex-wrap: nowrap;
 position: relative;
 `),m("tabs-tab-wrapper",`
 display: flex;
 flex-wrap: nowrap;
 flex-shrink: 0;
 flex-grow: 0;
 `),m("tabs-tab",`
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
 `,[$("disabled",{cursor:"not-allowed"}),O("close",`
 margin-left: 6px;
 transition:
 background-color .3s var(--n-bezier),
 color .3s var(--n-bezier);
 `),O("label",`
 display: flex;
 align-items: center;
 z-index: 1;
 `)]),m("tabs-bar",`
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
 `,[I("&.transition-disabled",`
 transition: none;
 `),$("disabled",`
 background-color: var(--n-tab-text-color-disabled)
 `)]),m("tabs-pane-wrapper",`
 position: relative;
 overflow: hidden;
 transition: max-height .2s var(--n-bezier);
 `),m("tab-pane",`
 color: var(--n-pane-text-color);
 width: 100%;
 transition:
 color .3s var(--n-bezier),
 background-color .3s var(--n-bezier),
 opacity .2s var(--n-bezier);
 left: 0;
 right: 0;
 top: 0;
 `,[I("&.next-transition-leave-active, &.prev-transition-leave-active, &.next-transition-enter-active, &.prev-transition-enter-active",`
 transition:
 color .3s var(--n-bezier),
 background-color .3s var(--n-bezier),
 transform .2s var(--n-bezier),
 opacity .2s var(--n-bezier);
 `),I("&.next-transition-leave-active, &.prev-transition-leave-active",`
 position: absolute;
 `),I("&.next-transition-enter-from, &.prev-transition-leave-to",`
 transform: translateX(32px);
 opacity: 0;
 `),I("&.next-transition-leave-to, &.prev-transition-enter-from",`
 transform: translateX(-32px);
 opacity: 0;
 `),I("&.next-transition-leave-from, &.next-transition-enter-to, &.prev-transition-leave-from, &.prev-transition-enter-to",`
 transform: translateX(0);
 opacity: 1;
 `)]),m("tabs-tab-pad",`
 box-sizing: border-box;
 width: var(--n-tab-gap);
 flex-grow: 0;
 flex-shrink: 0;
 `),$("line-type, bar-type",[m("tabs-tab",`
 font-weight: var(--n-tab-font-weight);
 box-sizing: border-box;
 vertical-align: bottom;
 `,[I("&:hover",{color:"var(--n-tab-text-color-hover)"}),$("active",`
 color: var(--n-tab-text-color-active);
 font-weight: var(--n-tab-font-weight-active);
 `),$("disabled",{color:"var(--n-tab-text-color-disabled)"})])]),m("tabs-nav",[$("line-type",[$("top",[O("prefix, suffix",`
 border-bottom: 1px solid var(--n-tab-border-color);
 `),m("tabs-nav-scroll-content",`
 border-bottom: 1px solid var(--n-tab-border-color);
 `),m("tabs-bar",`
 bottom: -1px;
 `)]),$("left",[O("prefix, suffix",`
 border-right: 1px solid var(--n-tab-border-color);
 `),m("tabs-nav-scroll-content",`
 border-right: 1px solid var(--n-tab-border-color);
 `),m("tabs-bar",`
 right: -1px;
 `)]),$("right",[O("prefix, suffix",`
 border-left: 1px solid var(--n-tab-border-color);
 `),m("tabs-nav-scroll-content",`
 border-left: 1px solid var(--n-tab-border-color);
 `),m("tabs-bar",`
 left: -1px;
 `)]),$("bottom",[O("prefix, suffix",`
 border-top: 1px solid var(--n-tab-border-color);
 `),m("tabs-nav-scroll-content",`
 border-top: 1px solid var(--n-tab-border-color);
 `),m("tabs-bar",`
 top: -1px;
 `)]),O("prefix, suffix",`
 transition: border-color .3s var(--n-bezier);
 `),m("tabs-nav-scroll-content",`
 transition: border-color .3s var(--n-bezier);
 `),m("tabs-bar",`
 border-radius: 0;
 `)]),$("card-type",[O("prefix, suffix",`
 transition: border-color .3s var(--n-bezier);
 `),m("tabs-pad",`
 flex-grow: 1;
 transition: border-color .3s var(--n-bezier);
 `),m("tabs-tab-pad",`
 transition: border-color .3s var(--n-bezier);
 `),m("tabs-tab",`
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
 `,[O("height-placeholder",`
 width: 0;
 font-size: var(--n-tab-font-size);
 `),pe("disabled",[I("&:hover",`
 color: var(--n-tab-text-color-hover);
 `)])]),$("closable","padding-right: 8px;"),$("active",`
 background-color: #0000;
 font-weight: var(--n-tab-font-weight-active);
 color: var(--n-tab-text-color-active);
 `),$("disabled","color: var(--n-tab-text-color-disabled);")])]),$("left, right",`
 flex-direction: column; 
 `,[O("prefix, suffix",`
 padding: var(--n-tab-padding-vertical);
 `),m("tabs-wrapper",`
 flex-direction: column;
 `),m("tabs-tab-wrapper",`
 flex-direction: column;
 `,[m("tabs-tab-pad",`
 height: var(--n-tab-gap-vertical);
 width: 100%;
 `)])]),$("top",[$("card-type",[m("tabs-scroll-padding","border-bottom: 1px solid var(--n-tab-border-color);"),O("prefix, suffix",`
 border-bottom: 1px solid var(--n-tab-border-color);
 `),m("tabs-tab",`
 border-top-left-radius: var(--n-tab-border-radius);
 border-top-right-radius: var(--n-tab-border-radius);
 `,[$("active",`
 border-bottom: 1px solid #0000;
 `)]),m("tabs-tab-pad",`
 border-bottom: 1px solid var(--n-tab-border-color);
 `),m("tabs-pad",`
 border-bottom: 1px solid var(--n-tab-border-color);
 `)])]),$("left",[$("card-type",[m("tabs-scroll-padding","border-right: 1px solid var(--n-tab-border-color);"),O("prefix, suffix",`
 border-right: 1px solid var(--n-tab-border-color);
 `),m("tabs-tab",`
 border-top-left-radius: var(--n-tab-border-radius);
 border-bottom-left-radius: var(--n-tab-border-radius);
 `,[$("active",`
 border-right: 1px solid #0000;
 `)]),m("tabs-tab-pad",`
 border-right: 1px solid var(--n-tab-border-color);
 `),m("tabs-pad",`
 border-right: 1px solid var(--n-tab-border-color);
 `)])]),$("right",[$("card-type",[m("tabs-scroll-padding","border-left: 1px solid var(--n-tab-border-color);"),O("prefix, suffix",`
 border-left: 1px solid var(--n-tab-border-color);
 `),m("tabs-tab",`
 border-top-right-radius: var(--n-tab-border-radius);
 border-bottom-right-radius: var(--n-tab-border-radius);
 `,[$("active",`
 border-left: 1px solid #0000;
 `)]),m("tabs-tab-pad",`
 border-left: 1px solid var(--n-tab-border-color);
 `),m("tabs-pad",`
 border-left: 1px solid var(--n-tab-border-color);
 `)])]),$("bottom",[$("card-type",[m("tabs-scroll-padding","border-top: 1px solid var(--n-tab-border-color);"),O("prefix, suffix",`
 border-top: 1px solid var(--n-tab-border-color);
 `),m("tabs-tab",`
 border-bottom-left-radius: var(--n-tab-border-radius);
 border-bottom-right-radius: var(--n-tab-border-radius);
 `,[$("active",`
 border-top: 1px solid #0000;
 `)]),m("tabs-tab-pad",`
 border-top: 1px solid var(--n-tab-border-color);
 `),m("tabs-pad",`
 border-top: 1px solid var(--n-tab-border-color);
 `)])])])]),Xi=Object.assign(Object.assign({},de.props),{value:[String,Number],defaultValue:[String,Number],trigger:{type:String,default:"click"},type:{type:String,default:"bar"},closable:Boolean,justifyContent:String,size:{type:String,default:"medium"},placement:{type:String,default:"top"},tabStyle:[String,Object],tabClass:String,addTabStyle:[String,Object],addTabClass:String,barWidth:Number,paneClass:String,paneStyle:[String,Object],paneWrapperClass:String,paneWrapperStyle:[String,Object],addable:[Boolean,Object],tabsPadding:{type:Number,default:0},animated:Boolean,onBeforeLeave:Function,onAdd:Function,"onUpdate:value":[Function,Array],onUpdateValue:[Function,Array],onClose:[Function,Array],labelSize:String,activeName:[String,Number],onActiveNameChange:[Function,Array]}),bs=G({name:"Tabs",props:Xi,setup(e,{slots:t}){var n,r,o,a;const{mergedClsPrefixRef:s,inlineThemeDisabled:l}=Ue(e),i=de("Tabs","-tabs",Vi,co,e,s),h=M(null),g=M(null),w=M(null),u=M(null),d=M(null),x=M(null),b=M(!0),v=M(!0),c=$t(e,["labelSize","size"]),T=$t(e,["activeName","value"]),L=M((r=(n=T.value)!==null&&n!==void 0?n:e.defaultValue)!==null&&r!==void 0?r:t.default?(a=(o=et(t.default())[0])===null||o===void 0?void 0:o.props)===null||a===void 0?void 0:a.name:null),B=_n(T,L),_={id:0},z=K(()=>{if(!(!e.justifyContent||e.type==="card"))return{display:"flex",justifyContent:e.justifyContent}});ie(B,()=>{_.id=0,V(),Y()});function S(){var f;const{value:p}=B;return p===null?null:(f=h.value)===null||f===void 0?void 0:f.querySelector(`[data-name="${p}"]`)}function A(f){if(e.type==="card")return;const{value:p}=g;if(!p)return;const P=p.style.opacity==="0";if(f){const k=`${s.value}-tabs-bar--disabled`,{barWidth:j,placement:ne}=e;if(f.dataset.disabled==="true"?p.classList.add(k):p.classList.remove(k),["top","bottom"].includes(ne)){if(E(["top","maxHeight","height"]),typeof j=="number"&&f.offsetWidth>=j){const re=Math.floor((f.offsetWidth-j)/2)+f.offsetLeft;p.style.left=`${re}px`,p.style.maxWidth=`${j}px`}else p.style.left=`${f.offsetLeft}px`,p.style.maxWidth=`${f.offsetWidth}px`;p.style.width="8192px",P&&(p.style.transition="none"),p.offsetWidth,P&&(p.style.transition="",p.style.opacity="1")}else{if(E(["left","maxWidth","width"]),typeof j=="number"&&f.offsetHeight>=j){const re=Math.floor((f.offsetHeight-j)/2)+f.offsetTop;p.style.top=`${re}px`,p.style.maxHeight=`${j}px`}else p.style.top=`${f.offsetTop}px`,p.style.maxHeight=`${f.offsetHeight}px`;p.style.height="8192px",P&&(p.style.transition="none"),p.offsetHeight,P&&(p.style.transition="",p.style.opacity="1")}}}function F(){if(e.type==="card")return;const{value:f}=g;f&&(f.style.opacity="0")}function E(f){const{value:p}=g;if(p)for(const P of f)p.style[P]=""}function V(){if(e.type==="card")return;const f=S();f?A(f):F()}function Y(){var f;const p=(f=d.value)===null||f===void 0?void 0:f.$el;if(!p)return;const P=S();if(!P)return;const{scrollLeft:k,offsetWidth:j}=p,{offsetLeft:ne,offsetWidth:re}=P;k>ne?p.scrollTo({top:0,left:ne,behavior:"smooth"}):ne+re>k+j&&p.scrollTo({top:0,left:ne+re-j,behavior:"smooth"})}const N=M(null);let q=0,C=null;function R(f){const p=N.value;if(p){q=f.getBoundingClientRect().height;const P=`${q}px`,k=()=>{p.style.height=P,p.style.maxHeight=P};C?(k(),C(),C=null):C=k}}function X(f){const p=N.value;if(p){const P=f.getBoundingClientRect().height,k=()=>{document.body.offsetHeight,p.style.maxHeight=`${P}px`,p.style.height=`${Math.max(q,P)}px`};C?(C(),C=null,k()):C=k}}function ae(){const f=N.value;if(f){f.style.maxHeight="",f.style.height="";const{paneWrapperStyle:p}=e;if(typeof p=="string")f.style.cssText=p;else if(p){const{maxHeight:P,height:k}=p;P!==void 0&&(f.style.maxHeight=P),k!==void 0&&(f.style.height=k)}}}const te={value:[]},Z=M("next");function Le(f){const p=B.value;let P="next";for(const k of te.value){if(k===p)break;if(k===f){P="prev";break}}Z.value=P,Xe(f)}function Xe(f){const{onActiveNameChange:p,onUpdateValue:P,"onUpdate:value":k}=e;p&&oe(p,f),P&&oe(P,f),k&&oe(k,f),L.value=f}function it(f){const{onClose:p}=e;p&&oe(p,f)}function Ke(){const{value:f}=g;if(!f)return;const p="transition-disabled";f.classList.add(p),V(),f.classList.remove(p)}const ce=M(null);function xe({transitionDisabled:f}){const p=h.value;if(!p)return;f&&p.classList.add("transition-disabled");const P=S();P&&ce.value&&(ce.value.style.width=`${P.offsetWidth}px`,ce.value.style.height=`${P.offsetHeight}px`,ce.value.style.transform=`translateX(${P.offsetLeft-uo(getComputedStyle(p).paddingLeft)}px)`,f&&ce.value.offsetWidth),f&&p.classList.remove("transition-disabled")}ie([B],()=>{e.type==="segment"&&Qe(()=>{xe({transitionDisabled:!1})})}),Ae(()=>{e.type==="segment"&&xe({transitionDisabled:!0})});let Ye=0;function st(f){var p;if(f.contentRect.width===0&&f.contentRect.height===0||Ye===f.contentRect.width)return;Ye=f.contentRect.width;const{type:P}=e;if((P==="line"||P==="bar")&&Ke(),P!=="segment"){const{placement:k}=e;dt((k==="top"||k==="bottom"?(p=d.value)===null||p===void 0?void 0:p.$el:x.value)||null)}}const lt=yt(st,64);ie([()=>e.justifyContent,()=>e.size],()=>{Qe(()=>{const{type:f}=e;(f==="line"||f==="bar")&&Ke()})});const ue=M(!1);function we(f){var p;const{target:P,contentRect:{width:k,height:j}}=f,ne=P.parentElement.parentElement.offsetWidth,re=P.parentElement.parentElement.offsetHeight,{placement:Se}=e;if(!ue.value)Se==="top"||Se==="bottom"?ne<k&&(ue.value=!0):re<j&&(ue.value=!0);else{const{value:Fe}=u;if(!Fe)return;Se==="top"||Se==="bottom"?ne-k>Fe.$el.offsetWidth&&(ue.value=!1):re-j>Fe.$el.offsetHeight&&(ue.value=!1)}dt(((p=d.value)===null||p===void 0?void 0:p.$el)||null)}const ir=yt(we,64);function sr(){const{onAdd:f}=e;f&&f(),Qe(()=>{const p=S(),{value:P}=d;!p||!P||P.scrollTo({left:p.offsetLeft,top:0,behavior:"smooth"})})}function dt(f){if(!f)return;const{placement:p}=e;if(p==="top"||p==="bottom"){const{scrollLeft:P,scrollWidth:k,offsetWidth:j}=f;b.value=P<=0,v.value=P+j>=k}else{const{scrollTop:P,scrollHeight:k,offsetHeight:j}=f;b.value=P<=0,v.value=P+j>=k}}const lr=yt(f=>{dt(f.target)},64);ve(Ft,{triggerRef:D(e,"trigger"),tabStyleRef:D(e,"tabStyle"),tabClassRef:D(e,"tabClass"),addTabStyleRef:D(e,"addTabStyle"),addTabClassRef:D(e,"addTabClass"),paneClassRef:D(e,"paneClass"),paneStyleRef:D(e,"paneStyle"),mergedClsPrefixRef:s,typeRef:D(e,"type"),closableRef:D(e,"closable"),valueRef:B,tabChangeIdRef:_,onBeforeLeaveRef:D(e,"onBeforeLeave"),activateTab:Le,handleClose:it,handleAdd:sr}),Wn(()=>{V(),Y()}),At(()=>{const{value:f}=w;if(!f)return;const{value:p}=s,P=`${p}-tabs-nav-scroll-wrapper--shadow-start`,k=`${p}-tabs-nav-scroll-wrapper--shadow-end`;b.value?f.classList.remove(P):f.classList.add(P),v.value?f.classList.remove(k):f.classList.add(k)});const dr={syncBarPosition:()=>{V()}},cr=()=>{xe({transitionDisabled:!0})},Wt=K(()=>{const{value:f}=c,{type:p}=e,P={card:"Card",bar:"Bar",line:"Line",segment:"Segment"}[p],k=`${f}${P}`,{self:{barColor:j,closeIconColor:ne,closeIconColorHover:re,closeIconColorPressed:Se,tabColor:Fe,tabBorderColor:ur,paneTextColor:fr,tabFontWeight:hr,tabBorderRadius:pr,tabFontWeightActive:br,colorSegment:vr,fontWeightStrong:gr,tabColorSegment:mr,closeSize:yr,closeIconSize:xr,closeColorHover:wr,closeColorPressed:Cr,closeBorderRadius:Sr,[H("panePadding",f)]:Ge,[H("tabPadding",k)]:$r,[H("tabPaddingVertical",k)]:Tr,[H("tabGap",k)]:Pr,[H("tabGap",`${k}Vertical`)]:zr,[H("tabTextColor",p)]:_r,[H("tabTextColorActive",p)]:Er,[H("tabTextColorHover",p)]:kr,[H("tabTextColorDisabled",p)]:Ir,[H("tabFontSize",f)]:Br},common:{cubicBezierEaseInOut:Mr}}=i.value;return{"--n-bezier":Mr,"--n-color-segment":vr,"--n-bar-color":j,"--n-tab-font-size":Br,"--n-tab-text-color":_r,"--n-tab-text-color-active":Er,"--n-tab-text-color-disabled":Ir,"--n-tab-text-color-hover":kr,"--n-pane-text-color":fr,"--n-tab-border-color":ur,"--n-tab-border-radius":pr,"--n-close-size":yr,"--n-close-icon-size":xr,"--n-close-color-hover":wr,"--n-close-color-pressed":Cr,"--n-close-border-radius":Sr,"--n-close-icon-color":ne,"--n-close-icon-color-hover":re,"--n-close-icon-color-pressed":Se,"--n-tab-color":Fe,"--n-tab-font-weight":hr,"--n-tab-font-weight-active":br,"--n-tab-padding":$r,"--n-tab-padding-vertical":Tr,"--n-tab-gap":Pr,"--n-tab-gap-vertical":zr,"--n-pane-padding-left":Re(Ge,"left"),"--n-pane-padding-right":Re(Ge,"right"),"--n-pane-padding-top":Re(Ge,"top"),"--n-pane-padding-bottom":Re(Ge,"bottom"),"--n-font-weight-strong":gr,"--n-tab-color-segment":mr}}),Ce=l?ot("tabs",K(()=>`${c.value[0]}${e.type[0]}`),Wt,e):void 0;return Object.assign({mergedClsPrefix:s,mergedValue:B,renderedNames:new Set,segmentCapsuleElRef:ce,tabsPaneWrapperRef:N,tabsElRef:h,barElRef:g,addTabInstRef:u,xScrollInstRef:d,scrollWrapperElRef:w,addTabFixed:ue,tabWrapperStyle:z,handleNavResize:lt,mergedSize:c,handleScroll:lr,handleTabsResize:ir,cssVars:l?void 0:Wt,themeClass:Ce==null?void 0:Ce.themeClass,animationDirection:Z,renderNameListRef:te,yScrollElRef:x,handleSegmentResize:cr,onAnimationBeforeLeave:R,onAnimationEnter:X,onAnimationAfterEnter:ae,onRender:Ce==null?void 0:Ce.onRender},dr)},render(){const{mergedClsPrefix:e,type:t,placement:n,addTabFixed:r,addable:o,mergedSize:a,renderNameListRef:s,onRender:l,paneWrapperClass:i,paneWrapperStyle:h,$slots:{default:g,prefix:w,suffix:u}}=this;l==null||l();const d=g?et(g()).filter(_=>_.type.__TAB_PANE__===!0):[],x=g?et(g()).filter(_=>_.type.__TAB__===!0):[],b=!x.length,v=t==="card",c=t==="segment",T=!v&&!c&&this.justifyContent;s.value=[];const L=()=>{const _=y("div",{style:this.tabWrapperStyle,class:`${e}-tabs-wrapper`},T?null:y("div",{class:`${e}-tabs-scroll-padding`,style:n==="top"||n==="bottom"?{width:`${this.tabsPadding}px`}:{height:`${this.tabsPadding}px`}}),b?d.map((z,S)=>(s.value.push(z.props.name),wt(y(It,Object.assign({},z.props,{internalCreatedByPane:!0,internalLeftPadded:S!==0&&(!T||T==="center"||T==="start"||T==="end")}),z.children?{default:z.children.tab}:void 0)))):x.map((z,S)=>(s.value.push(z.props.name),wt(S!==0&&!T?Cn(z):z))),!r&&o&&v?wn(o,(b?d.length:x.length)!==0):null,T?null:y("div",{class:`${e}-tabs-scroll-padding`,style:{width:`${this.tabsPadding}px`}}));return y("div",{ref:"tabsElRef",class:`${e}-tabs-nav-scroll-content`},v&&o?y(ut,{onResize:this.handleTabsResize},{default:()=>_}):_,v?y("div",{class:`${e}-tabs-pad`}):null,v?null:y("div",{ref:"barElRef",class:`${e}-tabs-bar`}))},B=c?"top":n;return y("div",{class:[`${e}-tabs`,this.themeClass,`${e}-tabs--${t}-type`,`${e}-tabs--${a}-size`,T&&`${e}-tabs--flex`,`${e}-tabs--${B}`],style:this.cssVars},y("div",{class:[`${e}-tabs-nav--${t}-type`,`${e}-tabs-nav--${B}`,`${e}-tabs-nav`]},be(w,_=>_&&y("div",{class:`${e}-tabs-nav__prefix`},_)),c?y(ut,{onResize:this.handleSegmentResize},{default:()=>y("div",{class:`${e}-tabs-rail`,ref:"tabsElRef"},y("div",{class:`${e}-tabs-capsule`,ref:"segmentCapsuleElRef"},y("div",{class:`${e}-tabs-wrapper`},y("div",{class:`${e}-tabs-tab`}))),b?d.map((_,z)=>(s.value.push(_.props.name),y(It,Object.assign({},_.props,{internalCreatedByPane:!0,internalLeftPadded:z!==0}),_.children?{default:_.children.tab}:void 0))):x.map((_,z)=>(s.value.push(_.props.name),z===0?_:Cn(_))))}):y(ut,{onResize:this.handleNavResize},{default:()=>y("div",{class:`${e}-tabs-nav-scroll-wrapper`,ref:"scrollWrapperElRef"},["top","bottom"].includes(B)?y(Zo,{ref:"xScrollInstRef",onScroll:this.handleScroll},{default:L}):y("div",{class:`${e}-tabs-nav-y-scroll`,onScroll:this.handleScroll,ref:"yScrollElRef"},L()))}),r&&o&&v?wn(o,!0):null,be(u,_=>_&&y("div",{class:`${e}-tabs-nav__suffix`},_))),b&&(this.animated&&(B==="top"||B==="bottom")?y("div",{ref:"tabsPaneWrapperRef",style:h,class:[`${e}-tabs-pane-wrapper`,i]},xn(d,this.mergedValue,this.renderedNames,this.onAnimationBeforeLeave,this.onAnimationEnter,this.onAnimationAfterEnter,this.animationDirection)):xn(d,this.mergedValue,this.renderedNames)))}});function xn(e,t,n,r,o,a,s){const l=[];return e.forEach(i=>{const{name:h,displayDirective:g,"display-directive":w}=i.props,u=x=>g===x||w===x,d=t===h;if(i.key!==void 0&&(i.key=h),d||u("show")||u("show:lazy")&&n.has(h)){n.has(h)||n.add(h);const x=!u("if");l.push(x?Ve(i,[[An,d]]):i)}}),s?y(mo,{name:`${s}-transition`,onBeforeLeave:r,onEnter:o,onAfterEnter:a},{default:()=>l}):l}function wn(e,t){return y(It,{ref:"addTabInstRef",key:"__addable",name:"__addable",internalCreatedByPane:!0,internalAddable:!0,internalLeftPadded:t,disabled:typeof e=="object"&&e.disabled})}function Cn(e){const t=Ln(e);return t.props?t.props.internalLeftPadded=!0:t.props={internalLeftPadded:!0},t}function wt(e){return Array.isArray(e.dynamicProps)?e.dynamicProps.includes("internalLeftPadded")||e.dynamicProps.push("internalLeftPadded"):e.dynamicProps=["internalLeftPadded"],e}async function Sn(e,t){const{data:n}=await ye(e,{method:"POST",body:JSON.stringify(t)});return n}async function Ki(e,t){const{handle:n,ke1:r}=await wo(t),o=await Sn("/api/auth/stepup/init",{email:e,login_ke:r});let a;try{({ke3:a}=await Co(n,o.login_response,So,e))}catch{throw new Error("invalid credentials")}return(await Sn("/api/auth/stepup/finalize",{email:e,session_id:o.session_id,login_ke3:a})).step_up_token}async function Yi(){const{data:e}=await ye("/api/me");return e}async function vs(){const{data:e}=await ye("/api/me/sessions");return e}async function gs(e){await ye(`/api/me/sessions/${encodeURIComponent(e)}`,{method:"DELETE"})}async function ms(){const{data:e}=await ye("/api/me/sessions/sign-out-others",{method:"POST"});return e}async function ys(e,t){await ye("/api/me/password",{method:"POST",body:JSON.stringify({current_password:e,new_password:t})})}async function xs(e,t){const n=await Ki(e,t);await ye("/api/me",{method:"DELETE",headers:{"X-Step-Up-Token":n},body:JSON.stringify({email:e})})}const Gi={class:"topbar"},Zi={class:"brand-block"},qi={class:"brand"},Ji={class:"version"},Qi=["aria-label"],es=["aria-current"],ts=["aria-current"],ns=["aria-current"],rs=G({__name:"Topbar",props:{active:{}},setup(e){const t=M(null),n=M("dev"),{t:r}=fo(),o=K(()=>$o(n.value,r));Ae(async()=>{try{t.value=await Yi()}catch{}try{n.value=await To()}catch{}});async function a(){try{await Po()}catch{}finally{location.assign("/login.html")}}return(s,l)=>{var i;return Zt(),qt("header",Gi,[me("div",Zi,[me("div",qi,$e(Te(r)("common.appName")),1),me("div",Ji,$e(o.value),1)]),me("nav",{class:"topnav","aria-label":Te(r)("topbar.primaryNav")},[me("a",{href:"/",class:ft({active:s.active==="home"}),"aria-current":s.active==="home"?"page":!1},$e(Te(r)("topbar.home")),11,es),me("a",{href:"/settings.html",class:ft({active:s.active==="settings"}),"aria-current":s.active==="settings"?"page":!1},$e(Te(r)("topbar.settings")),11,ts),(i=t.value)!=null&&i.is_admin?(Zt(),qt("a",{key:0,href:"/admin/",class:ft({active:s.active==="admin"}),"aria-current":s.active==="admin"?"page":!1},$e(Te(r)("topbar.admin")),11,ns)):yo("",!0)],8,Qi),me("button",{type:"button",class:"ghost-btn",onClick:a},$e(Te(r)("topbar.signOut")),1)])}}}),ws=xo(rs,[["__scopeId","data-v-821f9efc"]]);export{Pi as A,Oo as B,hs as N,ws as T,Yo as V,Ai as a,ps as b,bs as c,us as d,Lo as e,_o as f,Ee as g,ys as h,Qt as i,Xn as j,xs as k,Rn as l,ds as m,Bo as n,cs as o,qn as p,vs as q,Hn as r,nr as s,Dn as t,_i as u,gs as v,ms as w,Be as x,$t as y,fs as z};
