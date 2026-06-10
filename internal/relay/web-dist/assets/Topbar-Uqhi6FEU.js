import{aj as uo,P as Co,G as a,v as po,y as p,x,z as B,u as $,ah as q,bA as J,aC as P,N as fo,bs as D,bT as mo,b$ as Q,bn as ko,bO as yo,bY as xo,Z as U,a7 as b,az as Po,c0 as Io,L as G,A as So,a6 as zo,bf as _o,bh as Y,a4 as Z,a1 as f,bN as I,bS as S,b8 as V,a3 as Bo,bV as $o,_ as Ho}from"./mobile-guard-DqtBEVks.js";import{a as z,b as To,f as wo,c as Eo}from"./pwa-kbA0o_z-.js";function Mo(o){const{textColor2:c,primaryColorHover:r,primaryColorPressed:d,primaryColor:n,infoColor:h,successColor:s,warningColor:i,errorColor:l,baseColor:m,borderColor:k,opacityDisabled:g,tagColor:u,closeIconColor:e,closeIconColorHover:t,closeIconColorPressed:C,borderRadiusSmall:v,fontSizeMini:y,fontSizeTiny:H,fontSizeSmall:T,fontSizeMedium:w,heightMini:E,heightTiny:M,heightSmall:R,heightMedium:O,closeColorHover:N,closeColorPressed:W,buttonColor2Hover:j,buttonColor2Pressed:F,fontWeightStrong:L}=o;return Object.assign(Object.assign({},Co),{closeBorderRadius:v,heightTiny:E,heightSmall:M,heightMedium:R,heightLarge:O,borderRadius:v,opacityDisabled:g,fontSizeTiny:y,fontSizeSmall:H,fontSizeMedium:T,fontSizeLarge:w,fontWeightStrong:L,textColorCheckable:c,textColorHoverCheckable:c,textColorPressedCheckable:c,textColorChecked:m,colorCheckable:"#0000",colorHoverCheckable:j,colorPressedCheckable:F,colorChecked:n,colorCheckedHover:r,colorCheckedPressed:d,border:`1px solid ${k}`,textColor:c,color:u,colorBordered:"rgb(250, 250, 252)",closeIconColor:e,closeIconColorHover:t,closeIconColorPressed:C,closeColorHover:N,closeColorPressed:W,borderPrimary:`1px solid ${a(n,{alpha:.3})}`,textColorPrimary:n,colorPrimary:a(n,{alpha:.12}),colorBorderedPrimary:a(n,{alpha:.1}),closeIconColorPrimary:n,closeIconColorHoverPrimary:n,closeIconColorPressedPrimary:n,closeColorHoverPrimary:a(n,{alpha:.12}),closeColorPressedPrimary:a(n,{alpha:.18}),borderInfo:`1px solid ${a(h,{alpha:.3})}`,textColorInfo:h,colorInfo:a(h,{alpha:.12}),colorBorderedInfo:a(h,{alpha:.1}),closeIconColorInfo:h,closeIconColorHoverInfo:h,closeIconColorPressedInfo:h,closeColorHoverInfo:a(h,{alpha:.12}),closeColorPressedInfo:a(h,{alpha:.18}),borderSuccess:`1px solid ${a(s,{alpha:.3})}`,textColorSuccess:s,colorSuccess:a(s,{alpha:.12}),colorBorderedSuccess:a(s,{alpha:.1}),closeIconColorSuccess:s,closeIconColorHoverSuccess:s,closeIconColorPressedSuccess:s,closeColorHoverSuccess:a(s,{alpha:.12}),closeColorPressedSuccess:a(s,{alpha:.18}),borderWarning:`1px solid ${a(i,{alpha:.35})}`,textColorWarning:i,colorWarning:a(i,{alpha:.15}),colorBorderedWarning:a(i,{alpha:.12}),closeIconColorWarning:i,closeIconColorHoverWarning:i,closeIconColorPressedWarning:i,closeColorHoverWarning:a(i,{alpha:.12}),closeColorPressedWarning:a(i,{alpha:.18}),borderError:`1px solid ${a(l,{alpha:.23})}`,textColorError:l,colorError:a(l,{alpha:.1}),colorBorderedError:a(l,{alpha:.08}),closeIconColorError:l,closeIconColorHoverError:l,closeIconColorPressedError:l,closeColorHoverError:a(l,{alpha:.12}),closeColorPressedError:a(l,{alpha:.18})})}const Ro={common:uo,self:Mo},Oo={color:Object,type:{type:String,default:"default"},round:Boolean,size:{type:String,default:"medium"},closable:Boolean,disabled:{type:Boolean,default:void 0}},No=po("tag",`
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
 `),x("border",`
 pointer-events: none;
 position: absolute;
 left: 0;
 right: 0;
 top: 0;
 bottom: 0;
 border-radius: inherit;
 border: var(--n-border);
 transition: border-color .3s var(--n-bezier);
 `),x("icon",`
 display: flex;
 margin: 0 4px 0 0;
 color: var(--n-text-color);
 transition: color .3s var(--n-bezier);
 font-size: var(--n-avatar-size-override);
 `),x("avatar",`
 display: flex;
 margin: 0 6px 0 0;
 `),x("close",`
 margin: var(--n-close-margin);
 transition:
 background-color .3s var(--n-bezier),
 color .3s var(--n-bezier);
 `),p("round",`
 padding: 0 calc(var(--n-height) / 3);
 border-radius: calc(var(--n-height) / 2);
 `,[x("icon",`
 margin: 0 4px 0 calc((var(--n-height) - 8px) / -2);
 `),x("avatar",`
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
 `,[B("disabled",[$("&:hover","background-color: var(--n-color-hover-checkable);",[B("checked","color: var(--n-text-color-hover-checkable);")]),$("&:active","background-color: var(--n-color-pressed-checkable);",[B("checked","color: var(--n-text-color-pressed-checkable);")])]),p("checked",`
 color: var(--n-text-color-checked);
 background-color: var(--n-color-checked);
 `,[B("disabled",[$("&:hover","background-color: var(--n-color-checked-hover);"),$("&:active","background-color: var(--n-color-checked-pressed);")])])])]),Wo=Object.assign(Object.assign(Object.assign({},Q.props),Oo),{bordered:{type:Boolean,default:void 0},checked:Boolean,checkable:Boolean,strong:Boolean,triggerClickOnClose:Boolean,onClose:[Array,Function],onMouseenter:Function,onMouseleave:Function,"onUpdate:checked":Function,onUpdateChecked:Function,internalCloseFocusable:{type:Boolean,default:!0},internalCloseIsButtonTag:{type:Boolean,default:!0},onCheckedChange:Function}),jo=zo("n-tag"),Qo=q({name:"Tag",props:Wo,setup(o){const c=D(null),{mergedBorderedRef:r,mergedClsPrefixRef:d,inlineThemeDisabled:n,mergedRtlRef:h}=mo(o),s=Q("Tag","-tag",No,Ro,o,d);ko(jo,{roundRef:yo(o,"round")});function i(){if(!o.disabled&&o.checkable){const{checked:e,onCheckedChange:t,onUpdateChecked:C,"onUpdate:checked":v}=o;C&&C(!e),v&&v(!e),t&&t(!e)}}function l(e){if(o.triggerClickOnClose||e.stopPropagation(),!o.disabled){const{onClose:t}=o;t&&So(t,e)}}const m={setTextContent(e){const{value:t}=c;t&&(t.textContent=e)}},k=xo("Tag",h,d),g=U(()=>{const{type:e,size:t,color:{color:C,textColor:v}={}}=o,{common:{cubicBezierEaseInOut:y},self:{padding:H,closeMargin:T,borderRadius:w,opacityDisabled:E,textColorCheckable:M,textColorHoverCheckable:R,textColorPressedCheckable:O,textColorChecked:N,colorCheckable:W,colorHoverCheckable:j,colorPressedCheckable:F,colorChecked:L,colorCheckedHover:X,colorCheckedPressed:oo,closeBorderRadius:eo,fontWeightStrong:ro,[b("colorBordered",e)]:ao,[b("closeSize",t)]:so,[b("closeIconSize",t)]:co,[b("fontSize",t)]:no,[b("height",t)]:A,[b("color",e)]:lo,[b("textColor",e)]:to,[b("border",e)]:io,[b("closeIconColor",e)]:K,[b("closeIconColorHover",e)]:ho,[b("closeIconColorPressed",e)]:bo,[b("closeColorHover",e)]:go,[b("closeColorPressed",e)]:vo}}=s.value,_=Po(T);return{"--n-font-weight-strong":ro,"--n-avatar-size-override":`calc(${A} - 8px)`,"--n-bezier":y,"--n-border-radius":w,"--n-border":io,"--n-close-icon-size":co,"--n-close-color-pressed":vo,"--n-close-color-hover":go,"--n-close-border-radius":eo,"--n-close-icon-color":K,"--n-close-icon-color-hover":ho,"--n-close-icon-color-pressed":bo,"--n-close-icon-color-disabled":K,"--n-close-margin-top":_.top,"--n-close-margin-right":_.right,"--n-close-margin-bottom":_.bottom,"--n-close-margin-left":_.left,"--n-close-size":so,"--n-color":C||(r.value?ao:lo),"--n-color-checkable":W,"--n-color-checked":L,"--n-color-checked-hover":X,"--n-color-checked-pressed":oo,"--n-color-hover-checkable":j,"--n-color-pressed-checkable":F,"--n-font-size":no,"--n-height":A,"--n-opacity-disabled":E,"--n-padding":H,"--n-text-color":v||to,"--n-text-color-checkable":M,"--n-text-color-checked":N,"--n-text-color-hover-checkable":R,"--n-text-color-pressed-checkable":O}}),u=n?Io("tag",U(()=>{let e="";const{type:t,size:C,color:{color:v,textColor:y}={}}=o;return e+=t[0],e+=C[0],v&&(e+=`a${G(v)}`),y&&(e+=`b${G(y)}`),r.value&&(e+="c"),e}),g,o):void 0;return Object.assign(Object.assign({},m),{rtlEnabled:k,mergedClsPrefix:d,contentRef:c,mergedBordered:r,handleClick:i,handleCloseClick:l,cssVars:n?void 0:g,themeClass:u==null?void 0:u.themeClass,onRender:u==null?void 0:u.onRender})},render(){var o,c;const{mergedClsPrefix:r,rtlEnabled:d,closable:n,color:{borderColor:h}={},round:s,onRender:i,$slots:l}=this;i==null||i();const m=J(l.avatar,g=>g&&P("div",{class:`${r}-tag__avatar`},g)),k=J(l.icon,g=>g&&P("div",{class:`${r}-tag__icon`},g));return P("div",{class:[`${r}-tag`,this.themeClass,{[`${r}-tag--rtl`]:d,[`${r}-tag--strong`]:this.strong,[`${r}-tag--disabled`]:this.disabled,[`${r}-tag--checkable`]:this.checkable,[`${r}-tag--checked`]:this.checkable&&this.checked,[`${r}-tag--round`]:s,[`${r}-tag--avatar`]:m,[`${r}-tag--icon`]:k,[`${r}-tag--closable`]:n}],style:this.cssVars,onClick:this.handleClick,onMouseenter:this.onMouseenter,onMouseleave:this.onMouseleave},k||m,P("span",{class:`${r}-tag__content`,ref:"contentRef"},(c=(o=this.$slots).default)===null||c===void 0?void 0:c.call(o)),!this.checkable&&n?P(fo,{clsPrefix:r,class:`${r}-tag__close`,disabled:this.disabled,onClick:this.handleCloseClick,focusable:this.internalCloseFocusable,round:s,isButtonTag:this.internalCloseIsButtonTag,absolute:!0}):null,!this.checkable&&this.mergedBordered?P("div",{class:`${r}-tag__border`,style:{borderColor:h}}):null)}});async function Fo(){const{data:o}=await z("/api/me");return o}async function Xo(){const{data:o}=await z("/api/me/sessions");return o}async function oe(o){await z(`/api/me/sessions/${encodeURIComponent(o)}`,{method:"DELETE"})}async function ee(){const{data:o}=await z("/api/me/sessions/sign-out-others",{method:"POST"});return o}async function re(o,c){await z("/api/me/password",{method:"POST",body:JSON.stringify({current_password:o,new_password:c})})}async function ae(o,c){await z("/api/me",{method:"DELETE",body:JSON.stringify({email:o,password:c})})}const Lo={class:"topbar"},Vo={class:"brand-block"},Do={class:"brand"},Uo={class:"version"},Ao=["aria-label"],Ko=["aria-current"],Jo=["aria-current"],Go=["aria-current"],Yo=q({__name:"Topbar",props:{active:{}},setup(o){const c=D(null),r=D("dev"),{t:d}=$o(),n=U(()=>To(r.value,d));_o(async()=>{try{c.value=await Fo()}catch{}try{r.value=await wo()}catch{}});async function h(){try{await Eo()}catch{}finally{location.assign("/login.html")}}return(s,i)=>{var l;return Y(),Z("header",Lo,[f("div",Vo,[f("div",Do,I(S(d)("common.appName")),1),f("div",Uo,I(n.value),1)]),f("nav",{class:"topnav","aria-label":S(d)("topbar.primaryNav")},[f("a",{href:"/",class:V({active:s.active==="home"}),"aria-current":s.active==="home"?"page":!1},I(S(d)("topbar.home")),11,Ko),f("a",{href:"/settings.html",class:V({active:s.active==="settings"}),"aria-current":s.active==="settings"?"page":!1},I(S(d)("topbar.settings")),11,Jo),(l=c.value)!=null&&l.is_admin?(Y(),Z("a",{key:0,href:"/admin/",class:V({active:s.active==="admin"}),"aria-current":s.active==="admin"?"page":!1},I(S(d)("topbar.admin")),11,Go)):Bo("",!0)],8,Ao),f("button",{type:"button",class:"ghost-btn",onClick:h},I(S(d)("topbar.signOut")),1)])}}}),se=Ho(Yo,[["__scopeId","data-v-821f9efc"]]);export{Qo as N,se as T,re as c,ae as d,Xo as l,oe as r,ee as s};
