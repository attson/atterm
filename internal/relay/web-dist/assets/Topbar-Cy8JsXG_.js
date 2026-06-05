import{aj as vo,P as Co,G as r,v as uo,y as p,x as y,z as S,u as B,ah as q,bA as K,aC as I,N as po,bs as L,bT as fo,b$ as J,bn as mo,bO as ko,bY as xo,Z as D,a7 as b,az as yo,c0 as Io,L as G,A as Po,a6 as zo,bf as _o,bh as Y,a4 as Z,a1 as f,bN as P,bS as z,b8 as F,a3 as So,bV as Bo,_ as $o}from"./mobile-guard-D2cs6TNW.js";import{e as Ho,g as Mo,f as Ro,j as To}from"./pwa-Cr-wcQz2.js";function Eo(l){const{textColor2:h,primaryColorHover:e,primaryColorPressed:i,primaryColor:c,infoColor:d,successColor:a,warningColor:t,errorColor:s,baseColor:m,borderColor:k,opacityDisabled:g,tagColor:C,closeIconColor:o,closeIconColorHover:n,closeIconColorPressed:u,borderRadiusSmall:v,fontSizeMini:x,fontSizeTiny:$,fontSizeSmall:H,fontSizeMedium:M,heightMini:R,heightTiny:T,heightSmall:E,heightMedium:w,closeColorHover:O,closeColorPressed:j,buttonColor2Hover:N,buttonColor2Pressed:W,fontWeightStrong:V}=l;return Object.assign(Object.assign({},Co),{closeBorderRadius:v,heightTiny:R,heightSmall:T,heightMedium:E,heightLarge:w,borderRadius:v,opacityDisabled:g,fontSizeTiny:x,fontSizeSmall:$,fontSizeMedium:H,fontSizeLarge:M,fontWeightStrong:V,textColorCheckable:h,textColorHoverCheckable:h,textColorPressedCheckable:h,textColorChecked:m,colorCheckable:"#0000",colorHoverCheckable:N,colorPressedCheckable:W,colorChecked:c,colorCheckedHover:e,colorCheckedPressed:i,border:`1px solid ${k}`,textColor:h,color:C,colorBordered:"rgb(250, 250, 252)",closeIconColor:o,closeIconColorHover:n,closeIconColorPressed:u,closeColorHover:O,closeColorPressed:j,borderPrimary:`1px solid ${r(c,{alpha:.3})}`,textColorPrimary:c,colorPrimary:r(c,{alpha:.12}),colorBorderedPrimary:r(c,{alpha:.1}),closeIconColorPrimary:c,closeIconColorHoverPrimary:c,closeIconColorPressedPrimary:c,closeColorHoverPrimary:r(c,{alpha:.12}),closeColorPressedPrimary:r(c,{alpha:.18}),borderInfo:`1px solid ${r(d,{alpha:.3})}`,textColorInfo:d,colorInfo:r(d,{alpha:.12}),colorBorderedInfo:r(d,{alpha:.1}),closeIconColorInfo:d,closeIconColorHoverInfo:d,closeIconColorPressedInfo:d,closeColorHoverInfo:r(d,{alpha:.12}),closeColorPressedInfo:r(d,{alpha:.18}),borderSuccess:`1px solid ${r(a,{alpha:.3})}`,textColorSuccess:a,colorSuccess:r(a,{alpha:.12}),colorBorderedSuccess:r(a,{alpha:.1}),closeIconColorSuccess:a,closeIconColorHoverSuccess:a,closeIconColorPressedSuccess:a,closeColorHoverSuccess:r(a,{alpha:.12}),closeColorPressedSuccess:r(a,{alpha:.18}),borderWarning:`1px solid ${r(t,{alpha:.35})}`,textColorWarning:t,colorWarning:r(t,{alpha:.15}),colorBorderedWarning:r(t,{alpha:.12}),closeIconColorWarning:t,closeIconColorHoverWarning:t,closeIconColorPressedWarning:t,closeColorHoverWarning:r(t,{alpha:.12}),closeColorPressedWarning:r(t,{alpha:.18}),borderError:`1px solid ${r(s,{alpha:.23})}`,textColorError:s,colorError:r(s,{alpha:.1}),colorBorderedError:r(s,{alpha:.08}),closeIconColorError:s,closeIconColorHoverError:s,closeIconColorPressedError:s,closeColorHoverError:r(s,{alpha:.12}),closeColorPressedError:r(s,{alpha:.18})})}const wo={common:vo,self:Eo},Oo={color:Object,type:{type:String,default:"default"},round:Boolean,size:{type:String,default:"medium"},closable:Boolean,disabled:{type:Boolean,default:void 0}},jo=uo("tag",`
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
 `),y("border",`
 pointer-events: none;
 position: absolute;
 left: 0;
 right: 0;
 top: 0;
 bottom: 0;
 border-radius: inherit;
 border: var(--n-border);
 transition: border-color .3s var(--n-bezier);
 `),y("icon",`
 display: flex;
 margin: 0 4px 0 0;
 color: var(--n-text-color);
 transition: color .3s var(--n-bezier);
 font-size: var(--n-avatar-size-override);
 `),y("avatar",`
 display: flex;
 margin: 0 6px 0 0;
 `),y("close",`
 margin: var(--n-close-margin);
 transition:
 background-color .3s var(--n-bezier),
 color .3s var(--n-bezier);
 `),p("round",`
 padding: 0 calc(var(--n-height) / 3);
 border-radius: calc(var(--n-height) / 2);
 `,[y("icon",`
 margin: 0 4px 0 calc((var(--n-height) - 8px) / -2);
 `),y("avatar",`
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
 `,[S("disabled",[B("&:hover","background-color: var(--n-color-hover-checkable);",[S("checked","color: var(--n-text-color-hover-checkable);")]),B("&:active","background-color: var(--n-color-pressed-checkable);",[S("checked","color: var(--n-text-color-pressed-checkable);")])]),p("checked",`
 color: var(--n-text-color-checked);
 background-color: var(--n-color-checked);
 `,[S("disabled",[B("&:hover","background-color: var(--n-color-checked-hover);"),B("&:active","background-color: var(--n-color-checked-pressed);")])])])]),No=Object.assign(Object.assign(Object.assign({},J.props),Oo),{bordered:{type:Boolean,default:void 0},checked:Boolean,checkable:Boolean,strong:Boolean,triggerClickOnClose:Boolean,onClose:[Array,Function],onMouseenter:Function,onMouseleave:Function,"onUpdate:checked":Function,onUpdateChecked:Function,internalCloseFocusable:{type:Boolean,default:!0},internalCloseIsButtonTag:{type:Boolean,default:!0},onCheckedChange:Function}),Wo=zo("n-tag"),Jo=q({name:"Tag",props:No,setup(l){const h=L(null),{mergedBorderedRef:e,mergedClsPrefixRef:i,inlineThemeDisabled:c,mergedRtlRef:d}=fo(l),a=J("Tag","-tag",jo,wo,l,i);mo(Wo,{roundRef:ko(l,"round")});function t(){if(!l.disabled&&l.checkable){const{checked:o,onCheckedChange:n,onUpdateChecked:u,"onUpdate:checked":v}=l;u&&u(!o),v&&v(!o),n&&n(!o)}}function s(o){if(l.triggerClickOnClose||o.stopPropagation(),!l.disabled){const{onClose:n}=l;n&&Po(n,o)}}const m={setTextContent(o){const{value:n}=h;n&&(n.textContent=o)}},k=xo("Tag",d,i),g=D(()=>{const{type:o,size:n,color:{color:u,textColor:v}={}}=l,{common:{cubicBezierEaseInOut:x},self:{padding:$,closeMargin:H,borderRadius:M,opacityDisabled:R,textColorCheckable:T,textColorHoverCheckable:E,textColorPressedCheckable:w,textColorChecked:O,colorCheckable:j,colorHoverCheckable:N,colorPressedCheckable:W,colorChecked:V,colorCheckedHover:Q,colorCheckedPressed:X,closeBorderRadius:oo,fontWeightStrong:eo,[b("colorBordered",o)]:ro,[b("closeSize",n)]:ao,[b("closeIconSize",n)]:lo,[b("fontSize",n)]:co,[b("height",n)]:U,[b("color",o)]:so,[b("textColor",o)]:no,[b("border",o)]:to,[b("closeIconColor",o)]:A,[b("closeIconColorHover",o)]:io,[b("closeIconColorPressed",o)]:ho,[b("closeColorHover",o)]:bo,[b("closeColorPressed",o)]:go}}=a.value,_=yo(H);return{"--n-font-weight-strong":eo,"--n-avatar-size-override":`calc(${U} - 8px)`,"--n-bezier":x,"--n-border-radius":M,"--n-border":to,"--n-close-icon-size":lo,"--n-close-color-pressed":go,"--n-close-color-hover":bo,"--n-close-border-radius":oo,"--n-close-icon-color":A,"--n-close-icon-color-hover":io,"--n-close-icon-color-pressed":ho,"--n-close-icon-color-disabled":A,"--n-close-margin-top":_.top,"--n-close-margin-right":_.right,"--n-close-margin-bottom":_.bottom,"--n-close-margin-left":_.left,"--n-close-size":ao,"--n-color":u||(e.value?ro:so),"--n-color-checkable":j,"--n-color-checked":V,"--n-color-checked-hover":Q,"--n-color-checked-pressed":X,"--n-color-hover-checkable":N,"--n-color-pressed-checkable":W,"--n-font-size":co,"--n-height":U,"--n-opacity-disabled":R,"--n-padding":$,"--n-text-color":v||no,"--n-text-color-checkable":T,"--n-text-color-checked":O,"--n-text-color-hover-checkable":E,"--n-text-color-pressed-checkable":w}}),C=c?Io("tag",D(()=>{let o="";const{type:n,size:u,color:{color:v,textColor:x}={}}=l;return o+=n[0],o+=u[0],v&&(o+=`a${G(v)}`),x&&(o+=`b${G(x)}`),e.value&&(o+="c"),o}),g,l):void 0;return Object.assign(Object.assign({},m),{rtlEnabled:k,mergedClsPrefix:i,contentRef:h,mergedBordered:e,handleClick:t,handleCloseClick:s,cssVars:c?void 0:g,themeClass:C==null?void 0:C.themeClass,onRender:C==null?void 0:C.onRender})},render(){var l,h;const{mergedClsPrefix:e,rtlEnabled:i,closable:c,color:{borderColor:d}={},round:a,onRender:t,$slots:s}=this;t==null||t();const m=K(s.avatar,g=>g&&I("div",{class:`${e}-tag__avatar`},g)),k=K(s.icon,g=>g&&I("div",{class:`${e}-tag__icon`},g));return I("div",{class:[`${e}-tag`,this.themeClass,{[`${e}-tag--rtl`]:i,[`${e}-tag--strong`]:this.strong,[`${e}-tag--disabled`]:this.disabled,[`${e}-tag--checkable`]:this.checkable,[`${e}-tag--checked`]:this.checkable&&this.checked,[`${e}-tag--round`]:a,[`${e}-tag--avatar`]:m,[`${e}-tag--icon`]:k,[`${e}-tag--closable`]:c}],style:this.cssVars,onClick:this.handleClick,onMouseenter:this.onMouseenter,onMouseleave:this.onMouseleave},k||m,I("span",{class:`${e}-tag__content`,ref:"contentRef"},(h=(l=this.$slots).default)===null||h===void 0?void 0:h.call(l)),!this.checkable&&c?I(po,{clsPrefix:e,class:`${e}-tag__close`,disabled:this.disabled,onClick:this.handleCloseClick,focusable:this.internalCloseFocusable,round:a,isButtonTag:this.internalCloseIsButtonTag,absolute:!0}):null,!this.checkable&&this.mergedBordered?I("div",{class:`${e}-tag__border`,style:{borderColor:d}}):null)}}),Vo={class:"topbar"},Fo={class:"brand-block"},Lo={class:"brand"},Do={class:"version"},Uo=["aria-label"],Ao=["aria-current"],Ko=["aria-current"],Go=["aria-current"],Yo=q({__name:"Topbar",props:{active:{}},setup(l){const h=L(null),e=L("dev"),{t:i}=Bo(),c=D(()=>Ho(e.value,i));_o(async()=>{try{h.value=await Mo()}catch{}try{e.value=await Ro()}catch{}});async function d(){try{await To()}catch{}finally{location.assign("/login.html")}}return(a,t)=>{var s;return Y(),Z("header",Vo,[f("div",Fo,[f("div",Lo,P(z(i)("common.appName")),1),f("div",Do,P(c.value),1)]),f("nav",{class:"topnav","aria-label":z(i)("topbar.primaryNav")},[f("a",{href:"/",class:F({active:a.active==="home"}),"aria-current":a.active==="home"?"page":!1},P(z(i)("topbar.home")),11,Ao),f("a",{href:"/settings.html",class:F({active:a.active==="settings"}),"aria-current":a.active==="settings"?"page":!1},P(z(i)("topbar.settings")),11,Ko),(s=h.value)!=null&&s.is_admin?(Y(),Z("a",{key:0,href:"/admin/",class:F({active:a.active==="admin"}),"aria-current":a.active==="admin"?"page":!1},P(z(i)("topbar.admin")),11,Go)):So("",!0)],8,Uo),f("button",{type:"button",class:"ghost-btn",onClick:d},P(z(i)("topbar.signOut")),1)])}}}),Qo=$o(Yo,[["__scopeId","data-v-821f9efc"]]);export{Jo as N,Qo as T};
