import{ak as po,Q as fo,H as n,w as mo,z as p,y as S,A as B,v as w,ai as Z,bA as J,aC as I,N as ko,bs as D,bT as yo,b$ as oo,bn as xo,bO as So,bY as Io,$ as A,a8 as g,az as Po,c0 as zo,O as q,D as _o,a7 as Bo,bf as wo,bh as Y,a5 as Q,a2 as m,bN as P,bS as z,b8 as V,a4 as To,bV as Eo,n as $o}from"./mobile-guard-Cy2LCTJC.js";import{a as k}from"./client-BmcClgUp.js";import{b as Ho,n as Oo,K as Ro,S as Mo,a as No,f as Wo,d as Uo}from"./pwa-DOQQA-Sm.js";function jo(o){const{textColor2:a,primaryColorHover:e,primaryColorPressed:s,primaryColor:c,infoColor:i,successColor:t,warningColor:d,errorColor:l,baseColor:f,borderColor:y,opacityDisabled:b,tagColor:v,closeIconColor:r,closeIconColorHover:h,closeIconColorPressed:C,borderRadiusSmall:u,fontSizeMini:x,fontSizeTiny:T,fontSizeSmall:E,fontSizeMedium:$,heightMini:H,heightTiny:O,heightSmall:R,heightMedium:M,closeColorHover:N,closeColorPressed:W,buttonColor2Hover:U,buttonColor2Pressed:j,fontWeightStrong:F}=o;return Object.assign(Object.assign({},fo),{closeBorderRadius:u,heightTiny:H,heightSmall:O,heightMedium:R,heightLarge:M,borderRadius:u,opacityDisabled:b,fontSizeTiny:x,fontSizeSmall:T,fontSizeMedium:E,fontSizeLarge:$,fontWeightStrong:F,textColorCheckable:a,textColorHoverCheckable:a,textColorPressedCheckable:a,textColorChecked:f,colorCheckable:"#0000",colorHoverCheckable:U,colorPressedCheckable:j,colorChecked:c,colorCheckedHover:e,colorCheckedPressed:s,border:`1px solid ${y}`,textColor:a,color:v,colorBordered:"rgb(250, 250, 252)",closeIconColor:r,closeIconColorHover:h,closeIconColorPressed:C,closeColorHover:N,closeColorPressed:W,borderPrimary:`1px solid ${n(c,{alpha:.3})}`,textColorPrimary:c,colorPrimary:n(c,{alpha:.12}),colorBorderedPrimary:n(c,{alpha:.1}),closeIconColorPrimary:c,closeIconColorHoverPrimary:c,closeIconColorPressedPrimary:c,closeColorHoverPrimary:n(c,{alpha:.12}),closeColorPressedPrimary:n(c,{alpha:.18}),borderInfo:`1px solid ${n(i,{alpha:.3})}`,textColorInfo:i,colorInfo:n(i,{alpha:.12}),colorBorderedInfo:n(i,{alpha:.1}),closeIconColorInfo:i,closeIconColorHoverInfo:i,closeIconColorPressedInfo:i,closeColorHoverInfo:n(i,{alpha:.12}),closeColorPressedInfo:n(i,{alpha:.18}),borderSuccess:`1px solid ${n(t,{alpha:.3})}`,textColorSuccess:t,colorSuccess:n(t,{alpha:.12}),colorBorderedSuccess:n(t,{alpha:.1}),closeIconColorSuccess:t,closeIconColorHoverSuccess:t,closeIconColorPressedSuccess:t,closeColorHoverSuccess:n(t,{alpha:.12}),closeColorPressedSuccess:n(t,{alpha:.18}),borderWarning:`1px solid ${n(d,{alpha:.35})}`,textColorWarning:d,colorWarning:n(d,{alpha:.15}),colorBorderedWarning:n(d,{alpha:.12}),closeIconColorWarning:d,closeIconColorHoverWarning:d,closeIconColorPressedWarning:d,closeColorHoverWarning:n(d,{alpha:.12}),closeColorPressedWarning:n(d,{alpha:.18}),borderError:`1px solid ${n(l,{alpha:.23})}`,textColorError:l,colorError:n(l,{alpha:.1}),colorBorderedError:n(l,{alpha:.08}),closeIconColorError:l,closeIconColorHoverError:l,closeIconColorPressedError:l,closeColorHoverError:n(l,{alpha:.12}),closeColorPressedError:n(l,{alpha:.18})})}const Fo={common:po,self:jo},Vo={color:Object,type:{type:String,default:"default"},round:Boolean,size:{type:String,default:"medium"},closable:Boolean,disabled:{type:Boolean,default:void 0}},Do=mo("tag",`
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
`,[p("strong",`
 font-weight: var(--n-font-weight-strong);
 `),S("border",`
 pointer-events: none;
 position: absolute;
 left: 0;
 right: 0;
 top: 0;
 bottom: 0;
 border-radius: inherit;
 border: var(--n-border);
 transition: border-color .3s var(--n-bezier);
 `),S("icon",`
 display: flex;
 margin: 0 4px 0 0;
 color: var(--n-text-color);
 transition: color .3s var(--n-bezier);
 font-size: var(--n-avatar-size-override);
 `),S("avatar",`
 display: flex;
 margin: 0 6px 0 0;
 `),S("close",`
 margin: var(--n-close-margin);
 transition:
 background-color .3s var(--n-bezier),
 color .3s var(--n-bezier);
 `),p("round",`
 padding: 0 calc(var(--n-height) / 3);
 border-radius: calc(var(--n-height) / 2);
 `,[S("icon",`
 margin: 0 4px 0 calc((var(--n-height) - 8px) / -2);
 `),S("avatar",`
 margin: 0 6px 0 calc((var(--n-height) - 8px) / -2);
 `),p("closable",`
 padding: 0 calc(var(--n-height) / 4) 0 calc(var(--n-height) / 3);
 `)]),p("icon, avatar",[p("round",`
 padding: 0 calc(var(--n-height) / 3) 0 calc(var(--n-height) / 2);
 `)]),p("disabled",`
 cursor: not-allowed !important;
 opacity: var(--n-opacity-disabled);
 `),p("checkable",`
 cursor: pointer;
 box-shadow: none;
 color: var(--n-text-color-checkable);
 background-color: var(--n-color-checkable);
 `,[B("disabled",[w("&:hover","background-color: var(--n-color-hover-checkable);",[B("checked","color: var(--n-text-color-hover-checkable);")]),w("&:active","background-color: var(--n-color-pressed-checkable);",[B("checked","color: var(--n-text-color-pressed-checkable);")])]),p("checked",`
 color: var(--n-text-color-checked);
 background-color: var(--n-color-checked);
 `,[B("disabled",[w("&:hover","background-color: var(--n-color-checked-hover);"),w("&:active","background-color: var(--n-color-checked-pressed);")])])])]),Ao=Object.assign(Object.assign(Object.assign({},oo.props),Vo),{bordered:{type:Boolean,default:void 0},checked:Boolean,checkable:Boolean,strong:Boolean,triggerClickOnClose:Boolean,onClose:[Array,Function],onMouseenter:Function,onMouseleave:Function,"onUpdate:checked":Function,onUpdateChecked:Function,internalCloseFocusable:{type:Boolean,default:!0},internalCloseIsButtonTag:{type:Boolean,default:!0},onCheckedChange:Function}),Lo=Bo("n-tag"),le=Z({name:"Tag",props:Ao,setup(o){const a=D(null),{mergedBorderedRef:e,mergedClsPrefixRef:s,inlineThemeDisabled:c,mergedRtlRef:i}=yo(o),t=oo("Tag","-tag",Do,Fo,o,s);xo(Lo,{roundRef:So(o,"round")});function d(){if(!o.disabled&&o.checkable){const{checked:r,onCheckedChange:h,onUpdateChecked:C,"onUpdate:checked":u}=o;C&&C(!r),u&&u(!r),h&&h(!r)}}function l(r){if(o.triggerClickOnClose||r.stopPropagation(),!o.disabled){const{onClose:h}=o;h&&_o(h,r)}}const f={setTextContent(r){const{value:h}=a;h&&(h.textContent=r)}},y=Io("Tag",i,s),b=A(()=>{const{type:r,size:h,color:{color:C,textColor:u}={}}=o,{common:{cubicBezierEaseInOut:x},self:{padding:T,closeMargin:E,borderRadius:$,opacityDisabled:H,textColorCheckable:O,textColorHoverCheckable:R,textColorPressedCheckable:M,textColorChecked:N,colorCheckable:W,colorHoverCheckable:U,colorPressedCheckable:j,colorChecked:F,colorCheckedHover:eo,colorCheckedPressed:ro,closeBorderRadius:ao,fontWeightStrong:no,[g("colorBordered",r)]:so,[g("closeSize",h)]:to,[g("closeIconSize",h)]:co,[g("fontSize",h)]:lo,[g("height",h)]:L,[g("color",r)]:io,[g("textColor",r)]:ho,[g("border",r)]:go,[g("closeIconColor",r)]:K,[g("closeIconColorHover",r)]:bo,[g("closeIconColorPressed",r)]:uo,[g("closeColorHover",r)]:vo,[g("closeColorPressed",r)]:Co}}=t.value,_=Po(E);return{"--n-font-weight-strong":no,"--n-avatar-size-override":`calc(${L} - 8px)`,"--n-bezier":x,"--n-border-radius":$,"--n-border":go,"--n-close-icon-size":co,"--n-close-color-pressed":Co,"--n-close-color-hover":vo,"--n-close-border-radius":ao,"--n-close-icon-color":K,"--n-close-icon-color-hover":bo,"--n-close-icon-color-pressed":uo,"--n-close-icon-color-disabled":K,"--n-close-margin-top":_.top,"--n-close-margin-right":_.right,"--n-close-margin-bottom":_.bottom,"--n-close-margin-left":_.left,"--n-close-size":to,"--n-color":C||(e.value?so:io),"--n-color-checkable":W,"--n-color-checked":F,"--n-color-checked-hover":eo,"--n-color-checked-pressed":ro,"--n-color-hover-checkable":U,"--n-color-pressed-checkable":j,"--n-font-size":lo,"--n-height":L,"--n-opacity-disabled":H,"--n-padding":T,"--n-text-color":u||ho,"--n-text-color-checkable":O,"--n-text-color-checked":N,"--n-text-color-hover-checkable":R,"--n-text-color-pressed-checkable":M}}),v=c?zo("tag",A(()=>{let r="";const{type:h,size:C,color:{color:u,textColor:x}={}}=o;return r+=h[0],r+=C[0],u&&(r+=`a${q(u)}`),x&&(r+=`b${q(x)}`),e.value&&(r+="c"),r}),b,o):void 0;return Object.assign(Object.assign({},f),{rtlEnabled:y,mergedClsPrefix:s,contentRef:a,mergedBordered:e,handleClick:d,handleCloseClick:l,cssVars:c?void 0:b,themeClass:v==null?void 0:v.themeClass,onRender:v==null?void 0:v.onRender})},render(){var o,a;const{mergedClsPrefix:e,rtlEnabled:s,closable:c,color:{borderColor:i}={},round:t,onRender:d,$slots:l}=this;d==null||d();const f=J(l.avatar,b=>b&&I("div",{class:`${e}-tag__avatar`},b)),y=J(l.icon,b=>b&&I("div",{class:`${e}-tag__icon`},b));return I("div",{class:[`${e}-tag`,this.themeClass,{[`${e}-tag--rtl`]:s,[`${e}-tag--strong`]:this.strong,[`${e}-tag--disabled`]:this.disabled,[`${e}-tag--checkable`]:this.checkable,[`${e}-tag--checked`]:this.checkable&&this.checked,[`${e}-tag--round`]:t,[`${e}-tag--avatar`]:f,[`${e}-tag--icon`]:y,[`${e}-tag--closable`]:c}],style:this.cssVars,onClick:this.handleClick,onMouseenter:this.onMouseenter,onMouseleave:this.onMouseleave},y||f,I("span",{class:`${e}-tag__content`,ref:"contentRef"},(a=(o=this.$slots).default)===null||a===void 0?void 0:a.call(o)),!this.checkable&&c?I(ko,{clsPrefix:e,class:`${e}-tag__close`,disabled:this.disabled,onClick:this.handleCloseClick,focusable:this.internalCloseFocusable,round:t,isButtonTag:this.internalCloseIsButtonTag,absolute:!0}):null,!this.checkable&&this.mergedBordered?I("div",{class:`${e}-tag__border`,style:{borderColor:i}}):null)}});function Ko(o){return btoa(String.fromCharCode(...o))}function Jo(o){const a=atob(o),e=new Uint8Array(a.length);for(let s=0;s<a.length;s++)e[s]=a.charCodeAt(s);return e}function X(o){return Ko(new Uint8Array(o))}async function G(o,a){const{data:e}=await k(o,{method:"POST",body:JSON.stringify(a)});return e}async function qo(o,a){const e=Ho(),s=Oo(),c=await s.authInit(a);if(c instanceof Error)throw c;const i=await G("/api/auth/stepup/init",{email:o,login_ke:X(c.serialize())}),t=Array.from(Jo(i.login_response)),d=Ro.deserialize(e,t),l=await s.authFinish(d,Mo,o);if(l instanceof Error)throw new Error("invalid credentials");return(await G("/api/auth/stepup/finalize",{email:o,session_id:i.session_id,login_ke3:X(l.ke3.serialize())})).step_up_token}async function Yo(){const{data:o}=await k("/api/me");return o}async function ie(){const{data:o}=await k("/api/me/sessions");return o}async function de(o){await k(`/api/me/sessions/${encodeURIComponent(o)}`,{method:"DELETE"})}async function he(){const{data:o}=await k("/api/me/sessions/sign-out-others",{method:"POST"});return o}async function ge(o,a){await k("/api/me/password",{method:"POST",body:JSON.stringify({current_password:o,new_password:a})})}async function be(o,a){const e=await qo(o,a);await k("/api/me",{method:"DELETE",headers:{"X-Step-Up-Token":e},body:JSON.stringify({email:o})})}const Qo={class:"topbar"},Xo={class:"brand-block"},Go={class:"brand"},Zo={class:"version"},oe=["aria-label"],ee=["aria-current"],re=["aria-current"],ae=["aria-current"],ne=Z({__name:"Topbar",props:{active:{}},setup(o){const a=D(null),e=D("dev"),{t:s}=Eo(),c=A(()=>No(e.value,s));wo(async()=>{try{a.value=await Yo()}catch{}try{e.value=await Wo()}catch{}});async function i(){try{await Uo()}catch{}finally{location.assign("/login.html")}}return(t,d)=>{var l;return Y(),Q("header",Qo,[m("div",Xo,[m("div",Go,P(z(s)("common.appName")),1),m("div",Zo,P(c.value),1)]),m("nav",{class:"topnav","aria-label":z(s)("topbar.primaryNav")},[m("a",{href:"/",class:V({active:t.active==="home"}),"aria-current":t.active==="home"?"page":!1},P(z(s)("topbar.home")),11,ee),m("a",{href:"/settings.html",class:V({active:t.active==="settings"}),"aria-current":t.active==="settings"?"page":!1},P(z(s)("topbar.settings")),11,re),(l=a.value)!=null&&l.is_admin?(Y(),Q("a",{key:0,href:"/admin/",class:V({active:t.active==="admin"}),"aria-current":t.active==="admin"?"page":!1},P(z(s)("topbar.admin")),11,ae)):To("",!0)],8,oe),m("button",{type:"button",class:"ghost-btn",onClick:i},P(z(s)("topbar.signOut")),1)])}}}),ue=$o(ne,[["__scopeId","data-v-821f9efc"]]);export{le as N,ue as T,ge as c,be as d,ie as l,de as r,he as s};
