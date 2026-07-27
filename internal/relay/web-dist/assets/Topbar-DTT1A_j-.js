import{ak as po,Q as Co,H as a,w as fo,z as C,y as S,A as B,v as w,ai as G,bA as A,aC as I,N as mo,bs as D,bT as ko,b$ as Z,bn as yo,bO as xo,bY as So,$ as U,a8 as g,az as Io,c0 as Po,O as K,D as _o,a7 as zo,bf as Bo,bh as Y,a5 as Q,a2 as f,bN as P,bS as _,b8 as V,a4 as wo,bV as To,n as $o}from"./mobile-guard-s8DbgXeC.js";import{a as m}from"./client-B2P3lC0H.js";import{d as Ho,o as Eo,S as Oo,a as Ro,f as Mo,c as No}from"./pwa-nhAGcnUq.js";function Wo(o){const{textColor2:s,primaryColorHover:e,primaryColorPressed:l,primaryColor:c,infoColor:t,successColor:n,warningColor:h,errorColor:i,baseColor:k,borderColor:y,opacityDisabled:b,tagColor:v,closeIconColor:r,closeIconColorHover:d,closeIconColorPressed:p,borderRadiusSmall:u,fontSizeMini:x,fontSizeTiny:T,fontSizeSmall:$,fontSizeMedium:H,heightMini:E,heightTiny:O,heightSmall:R,heightMedium:M,closeColorHover:N,closeColorPressed:W,buttonColor2Hover:j,buttonColor2Pressed:F,fontWeightStrong:L}=o;return Object.assign(Object.assign({},Co),{closeBorderRadius:u,heightTiny:E,heightSmall:O,heightMedium:R,heightLarge:M,borderRadius:u,opacityDisabled:b,fontSizeTiny:x,fontSizeSmall:T,fontSizeMedium:$,fontSizeLarge:H,fontWeightStrong:L,textColorCheckable:s,textColorHoverCheckable:s,textColorPressedCheckable:s,textColorChecked:k,colorCheckable:"#0000",colorHoverCheckable:j,colorPressedCheckable:F,colorChecked:c,colorCheckedHover:e,colorCheckedPressed:l,border:`1px solid ${y}`,textColor:s,color:v,colorBordered:"rgb(250, 250, 252)",closeIconColor:r,closeIconColorHover:d,closeIconColorPressed:p,closeColorHover:N,closeColorPressed:W,borderPrimary:`1px solid ${a(c,{alpha:.3})}`,textColorPrimary:c,colorPrimary:a(c,{alpha:.12}),colorBorderedPrimary:a(c,{alpha:.1}),closeIconColorPrimary:c,closeIconColorHoverPrimary:c,closeIconColorPressedPrimary:c,closeColorHoverPrimary:a(c,{alpha:.12}),closeColorPressedPrimary:a(c,{alpha:.18}),borderInfo:`1px solid ${a(t,{alpha:.3})}`,textColorInfo:t,colorInfo:a(t,{alpha:.12}),colorBorderedInfo:a(t,{alpha:.1}),closeIconColorInfo:t,closeIconColorHoverInfo:t,closeIconColorPressedInfo:t,closeColorHoverInfo:a(t,{alpha:.12}),closeColorPressedInfo:a(t,{alpha:.18}),borderSuccess:`1px solid ${a(n,{alpha:.3})}`,textColorSuccess:n,colorSuccess:a(n,{alpha:.12}),colorBorderedSuccess:a(n,{alpha:.1}),closeIconColorSuccess:n,closeIconColorHoverSuccess:n,closeIconColorPressedSuccess:n,closeColorHoverSuccess:a(n,{alpha:.12}),closeColorPressedSuccess:a(n,{alpha:.18}),borderWarning:`1px solid ${a(h,{alpha:.35})}`,textColorWarning:h,colorWarning:a(h,{alpha:.15}),colorBorderedWarning:a(h,{alpha:.12}),closeIconColorWarning:h,closeIconColorHoverWarning:h,closeIconColorPressedWarning:h,closeColorHoverWarning:a(h,{alpha:.12}),closeColorPressedWarning:a(h,{alpha:.18}),borderError:`1px solid ${a(i,{alpha:.23})}`,textColorError:i,colorError:a(i,{alpha:.1}),colorBorderedError:a(i,{alpha:.08}),closeIconColorError:i,closeIconColorHoverError:i,closeIconColorPressedError:i,closeColorHoverError:a(i,{alpha:.12}),closeColorPressedError:a(i,{alpha:.18})})}const jo={common:po,self:Wo},Fo={color:Object,type:{type:String,default:"default"},round:Boolean,size:{type:String,default:"medium"},closable:Boolean,disabled:{type:Boolean,default:void 0}},Lo=fo("tag",`
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
`,[C("strong",`
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
 `),C("round",`
 padding: 0 calc(var(--n-height) / 3);
 border-radius: calc(var(--n-height) / 2);
 `,[S("icon",`
 margin: 0 4px 0 calc((var(--n-height) - 8px) / -2);
 `),S("avatar",`
 margin: 0 6px 0 calc((var(--n-height) - 8px) / -2);
 `),C("closable",`
 padding: 0 calc(var(--n-height) / 4) 0 calc(var(--n-height) / 3);
 `)]),C("icon, avatar",[C("round",`
 padding: 0 calc(var(--n-height) / 3) 0 calc(var(--n-height) / 2);
 `)]),C("disabled",`
 cursor: not-allowed !important;
 opacity: var(--n-opacity-disabled);
 `),C("checkable",`
 cursor: pointer;
 box-shadow: none;
 color: var(--n-text-color-checkable);
 background-color: var(--n-color-checkable);
 `,[B("disabled",[w("&:hover","background-color: var(--n-color-hover-checkable);",[B("checked","color: var(--n-text-color-hover-checkable);")]),w("&:active","background-color: var(--n-color-pressed-checkable);",[B("checked","color: var(--n-text-color-pressed-checkable);")])]),C("checked",`
 color: var(--n-text-color-checked);
 background-color: var(--n-color-checked);
 `,[B("disabled",[w("&:hover","background-color: var(--n-color-checked-hover);"),w("&:active","background-color: var(--n-color-checked-pressed);")])])])]),Vo=Object.assign(Object.assign(Object.assign({},Z.props),Fo),{bordered:{type:Boolean,default:void 0},checked:Boolean,checkable:Boolean,strong:Boolean,triggerClickOnClose:Boolean,onClose:[Array,Function],onMouseenter:Function,onMouseleave:Function,"onUpdate:checked":Function,onUpdateChecked:Function,internalCloseFocusable:{type:Boolean,default:!0},internalCloseIsButtonTag:{type:Boolean,default:!0},onCheckedChange:Function}),Do=zo("n-tag"),se=G({name:"Tag",props:Vo,setup(o){const s=D(null),{mergedBorderedRef:e,mergedClsPrefixRef:l,inlineThemeDisabled:c,mergedRtlRef:t}=ko(o),n=Z("Tag","-tag",Lo,jo,o,l);yo(Do,{roundRef:xo(o,"round")});function h(){if(!o.disabled&&o.checkable){const{checked:r,onCheckedChange:d,onUpdateChecked:p,"onUpdate:checked":u}=o;p&&p(!r),u&&u(!r),d&&d(!r)}}function i(r){if(o.triggerClickOnClose||r.stopPropagation(),!o.disabled){const{onClose:d}=o;d&&_o(d,r)}}const k={setTextContent(r){const{value:d}=s;d&&(d.textContent=r)}},y=So("Tag",t,l),b=U(()=>{const{type:r,size:d,color:{color:p,textColor:u}={}}=o,{common:{cubicBezierEaseInOut:x},self:{padding:T,closeMargin:$,borderRadius:H,opacityDisabled:E,textColorCheckable:O,textColorHoverCheckable:R,textColorPressedCheckable:M,textColorChecked:N,colorCheckable:W,colorHoverCheckable:j,colorPressedCheckable:F,colorChecked:L,colorCheckedHover:oo,colorCheckedPressed:eo,closeBorderRadius:ro,fontWeightStrong:ao,[g("colorBordered",r)]:so,[g("closeSize",d)]:no,[g("closeIconSize",d)]:co,[g("fontSize",d)]:to,[g("height",d)]:J,[g("color",r)]:lo,[g("textColor",r)]:io,[g("border",r)]:ho,[g("closeIconColor",r)]:q,[g("closeIconColorHover",r)]:go,[g("closeIconColorPressed",r)]:bo,[g("closeColorHover",r)]:uo,[g("closeColorPressed",r)]:vo}}=n.value,z=Io($);return{"--n-font-weight-strong":ao,"--n-avatar-size-override":`calc(${J} - 8px)`,"--n-bezier":x,"--n-border-radius":H,"--n-border":ho,"--n-close-icon-size":co,"--n-close-color-pressed":vo,"--n-close-color-hover":uo,"--n-close-border-radius":ro,"--n-close-icon-color":q,"--n-close-icon-color-hover":go,"--n-close-icon-color-pressed":bo,"--n-close-icon-color-disabled":q,"--n-close-margin-top":z.top,"--n-close-margin-right":z.right,"--n-close-margin-bottom":z.bottom,"--n-close-margin-left":z.left,"--n-close-size":no,"--n-color":p||(e.value?so:lo),"--n-color-checkable":W,"--n-color-checked":L,"--n-color-checked-hover":oo,"--n-color-checked-pressed":eo,"--n-color-hover-checkable":j,"--n-color-pressed-checkable":F,"--n-font-size":to,"--n-height":J,"--n-opacity-disabled":E,"--n-padding":T,"--n-text-color":u||io,"--n-text-color-checkable":O,"--n-text-color-checked":N,"--n-text-color-hover-checkable":R,"--n-text-color-pressed-checkable":M}}),v=c?Po("tag",U(()=>{let r="";const{type:d,size:p,color:{color:u,textColor:x}={}}=o;return r+=d[0],r+=p[0],u&&(r+=`a${K(u)}`),x&&(r+=`b${K(x)}`),e.value&&(r+="c"),r}),b,o):void 0;return Object.assign(Object.assign({},k),{rtlEnabled:y,mergedClsPrefix:l,contentRef:s,mergedBordered:e,handleClick:h,handleCloseClick:i,cssVars:c?void 0:b,themeClass:v==null?void 0:v.themeClass,onRender:v==null?void 0:v.onRender})},render(){var o,s;const{mergedClsPrefix:e,rtlEnabled:l,closable:c,color:{borderColor:t}={},round:n,onRender:h,$slots:i}=this;h==null||h();const k=A(i.avatar,b=>b&&I("div",{class:`${e}-tag__avatar`},b)),y=A(i.icon,b=>b&&I("div",{class:`${e}-tag__icon`},b));return I("div",{class:[`${e}-tag`,this.themeClass,{[`${e}-tag--rtl`]:l,[`${e}-tag--strong`]:this.strong,[`${e}-tag--disabled`]:this.disabled,[`${e}-tag--checkable`]:this.checkable,[`${e}-tag--checked`]:this.checkable&&this.checked,[`${e}-tag--round`]:n,[`${e}-tag--avatar`]:k,[`${e}-tag--icon`]:y,[`${e}-tag--closable`]:c}],style:this.cssVars,onClick:this.handleClick,onMouseenter:this.onMouseenter,onMouseleave:this.onMouseleave},y||k,I("span",{class:`${e}-tag__content`,ref:"contentRef"},(s=(o=this.$slots).default)===null||s===void 0?void 0:s.call(o)),!this.checkable&&c?I(mo,{clsPrefix:e,class:`${e}-tag__close`,disabled:this.disabled,onClick:this.handleCloseClick,focusable:this.internalCloseFocusable,round:n,isButtonTag:this.internalCloseIsButtonTag,absolute:!0}):null,!this.checkable&&this.mergedBordered?I("div",{class:`${e}-tag__border`,style:{borderColor:t}}):null)}});async function X(o,s){const{data:e}=await m(o,{method:"POST",body:JSON.stringify(s)});return e}async function Uo(o,s){const{handle:e,ke1:l}=await Ho(s),c=await X("/api/auth/stepup/init",{email:o,login_ke:l});let t;try{({ke3:t}=await Eo(e,c.login_response,Oo,o))}catch{throw new Error("invalid credentials")}return(await X("/api/auth/stepup/finalize",{email:o,session_id:c.session_id,login_ke3:t})).step_up_token}async function Jo(){const{data:o}=await m("/api/me");return o}async function ne(){const{data:o}=await m("/api/me/sessions");return o}async function ce(o){await m(`/api/me/sessions/${encodeURIComponent(o)}`,{method:"DELETE"})}async function te(){const{data:o}=await m("/api/me/sessions/sign-out-others",{method:"POST"});return o}async function le(o,s){await m("/api/me/password",{method:"POST",body:JSON.stringify({current_password:o,new_password:s})})}async function ie(o,s){const e=await Uo(o,s);await m("/api/me",{method:"DELETE",headers:{"X-Step-Up-Token":e},body:JSON.stringify({email:o})})}const qo={class:"topbar"},Ao={class:"brand-block"},Ko={class:"brand"},Yo={class:"version"},Qo=["aria-label"],Xo=["aria-current"],Go=["aria-current"],Zo=["aria-current"],oe=G({__name:"Topbar",props:{active:{}},setup(o){const s=D(null),e=D("dev"),{t:l}=To(),c=U(()=>Ro(e.value,l));Bo(async()=>{try{s.value=await Jo()}catch{}try{e.value=await Mo()}catch{}});async function t(){try{await No()}catch{}finally{location.assign("/login.html")}}return(n,h)=>{var i;return Y(),Q("header",qo,[f("div",Ao,[f("div",Ko,P(_(l)("common.appName")),1),f("div",Yo,P(c.value),1)]),f("nav",{class:"topnav","aria-label":_(l)("topbar.primaryNav")},[f("a",{href:"/",class:V({active:n.active==="home"}),"aria-current":n.active==="home"?"page":!1},P(_(l)("topbar.home")),11,Xo),f("a",{href:"/settings.html",class:V({active:n.active==="settings"}),"aria-current":n.active==="settings"?"page":!1},P(_(l)("topbar.settings")),11,Go),(i=s.value)!=null&&i.is_admin?(Y(),Q("a",{key:0,href:"/admin/",class:V({active:n.active==="admin"}),"aria-current":n.active==="admin"?"page":!1},P(_(l)("topbar.admin")),11,Zo)):wo("",!0)],8,Qo),f("button",{type:"button",class:"ghost-btn",onClick:t},P(_(l)("topbar.signOut")),1)])}}}),de=$o(oe,[["__scopeId","data-v-821f9efc"]]);export{se as N,de as T,le as c,ie as d,ne as l,ce as r,te as s};
