import{aj as go,Q as bo,H as r,v as vo,y as p,x as y,z,u as S,ag as J,bw as L,az as P,N as Co,bo as F,bP as uo,bW as Q,bj as po,bK as fo,bT as mo,Z as U,a7 as h,av as ko,bX as xo,O as K,D as yo,a6 as Po,bb as Io,aw as zo,aq as So,bd as A,a4 as q,a1 as f,bJ as _o,b4 as N,a3 as Bo,b0 as Ho,_ as $o}from"./tokens-DETGupTO.js";function To(l){const{textColor2:i,primaryColorHover:e,primaryColorPressed:v,primaryColor:a,infoColor:n,successColor:c,warningColor:t,errorColor:d,baseColor:m,borderColor:k,opacityDisabled:g,tagColor:C,closeIconColor:o,closeIconColorHover:s,closeIconColorPressed:u,borderRadiusSmall:b,fontSizeMini:x,fontSizeTiny:_,fontSizeSmall:B,fontSizeMedium:H,heightMini:$,heightTiny:T,heightSmall:M,heightMedium:R,closeColorHover:w,closeColorPressed:E,buttonColor2Hover:W,buttonColor2Pressed:j,fontWeightStrong:O}=l;return Object.assign(Object.assign({},bo),{closeBorderRadius:b,heightTiny:$,heightSmall:T,heightMedium:M,heightLarge:R,borderRadius:b,opacityDisabled:g,fontSizeTiny:x,fontSizeSmall:_,fontSizeMedium:B,fontSizeLarge:H,fontWeightStrong:O,textColorCheckable:i,textColorHoverCheckable:i,textColorPressedCheckable:i,textColorChecked:m,colorCheckable:"#0000",colorHoverCheckable:W,colorPressedCheckable:j,colorChecked:a,colorCheckedHover:e,colorCheckedPressed:v,border:`1px solid ${k}`,textColor:i,color:C,colorBordered:"rgb(250, 250, 252)",closeIconColor:o,closeIconColorHover:s,closeIconColorPressed:u,closeColorHover:w,closeColorPressed:E,borderPrimary:`1px solid ${r(a,{alpha:.3})}`,textColorPrimary:a,colorPrimary:r(a,{alpha:.12}),colorBorderedPrimary:r(a,{alpha:.1}),closeIconColorPrimary:a,closeIconColorHoverPrimary:a,closeIconColorPressedPrimary:a,closeColorHoverPrimary:r(a,{alpha:.12}),closeColorPressedPrimary:r(a,{alpha:.18}),borderInfo:`1px solid ${r(n,{alpha:.3})}`,textColorInfo:n,colorInfo:r(n,{alpha:.12}),colorBorderedInfo:r(n,{alpha:.1}),closeIconColorInfo:n,closeIconColorHoverInfo:n,closeIconColorPressedInfo:n,closeColorHoverInfo:r(n,{alpha:.12}),closeColorPressedInfo:r(n,{alpha:.18}),borderSuccess:`1px solid ${r(c,{alpha:.3})}`,textColorSuccess:c,colorSuccess:r(c,{alpha:.12}),colorBorderedSuccess:r(c,{alpha:.1}),closeIconColorSuccess:c,closeIconColorHoverSuccess:c,closeIconColorPressedSuccess:c,closeColorHoverSuccess:r(c,{alpha:.12}),closeColorPressedSuccess:r(c,{alpha:.18}),borderWarning:`1px solid ${r(t,{alpha:.35})}`,textColorWarning:t,colorWarning:r(t,{alpha:.15}),colorBorderedWarning:r(t,{alpha:.12}),closeIconColorWarning:t,closeIconColorHoverWarning:t,closeIconColorPressedWarning:t,closeColorHoverWarning:r(t,{alpha:.12}),closeColorPressedWarning:r(t,{alpha:.18}),borderError:`1px solid ${r(d,{alpha:.23})}`,textColorError:d,colorError:r(d,{alpha:.1}),colorBorderedError:r(d,{alpha:.08}),closeIconColorError:d,closeIconColorHoverError:d,closeIconColorPressedError:d,closeColorHoverError:r(d,{alpha:.12}),closeColorPressedError:r(d,{alpha:.18})})}const Mo={common:go,self:To},Ro={color:Object,type:{type:String,default:"default"},round:Boolean,size:{type:String,default:"medium"},closable:Boolean,disabled:{type:Boolean,default:void 0}},wo=vo("tag",`
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
 `,[z("disabled",[S("&:hover","background-color: var(--n-color-hover-checkable);",[z("checked","color: var(--n-text-color-hover-checkable);")]),S("&:active","background-color: var(--n-color-pressed-checkable);",[z("checked","color: var(--n-text-color-pressed-checkable);")])]),p("checked",`
 color: var(--n-text-color-checked);
 background-color: var(--n-color-checked);
 `,[z("disabled",[S("&:hover","background-color: var(--n-color-checked-hover);"),S("&:active","background-color: var(--n-color-checked-pressed);")])])])]),Eo=Object.assign(Object.assign(Object.assign({},Q.props),Ro),{bordered:{type:Boolean,default:void 0},checked:Boolean,checkable:Boolean,strong:Boolean,triggerClickOnClose:Boolean,onClose:[Array,Function],onMouseenter:Function,onMouseleave:Function,"onUpdate:checked":Function,onUpdateChecked:Function,internalCloseFocusable:{type:Boolean,default:!0},internalCloseIsButtonTag:{type:Boolean,default:!0},onCheckedChange:Function}),Wo=Po("n-tag"),Ao=J({name:"Tag",props:Eo,setup(l){const i=F(null),{mergedBorderedRef:e,mergedClsPrefixRef:v,inlineThemeDisabled:a,mergedRtlRef:n}=uo(l),c=Q("Tag","-tag",wo,Mo,l,v);po(Wo,{roundRef:fo(l,"round")});function t(){if(!l.disabled&&l.checkable){const{checked:o,onCheckedChange:s,onUpdateChecked:u,"onUpdate:checked":b}=l;u&&u(!o),b&&b(!o),s&&s(!o)}}function d(o){if(l.triggerClickOnClose||o.stopPropagation(),!l.disabled){const{onClose:s}=l;s&&yo(s,o)}}const m={setTextContent(o){const{value:s}=i;s&&(s.textContent=o)}},k=mo("Tag",n,v),g=U(()=>{const{type:o,size:s,color:{color:u,textColor:b}={}}=l,{common:{cubicBezierEaseInOut:x},self:{padding:_,closeMargin:B,borderRadius:H,opacityDisabled:$,textColorCheckable:T,textColorHoverCheckable:M,textColorPressedCheckable:R,textColorChecked:w,colorCheckable:E,colorHoverCheckable:W,colorPressedCheckable:j,colorChecked:O,colorCheckedHover:X,colorCheckedPressed:Z,closeBorderRadius:G,fontWeightStrong:Y,[h("colorBordered",o)]:oo,[h("closeSize",s)]:eo,[h("closeIconSize",s)]:ro,[h("fontSize",s)]:ao,[h("height",s)]:V,[h("color",o)]:lo,[h("textColor",o)]:co,[h("border",o)]:so,[h("closeIconColor",o)]:D,[h("closeIconColorHover",o)]:no,[h("closeIconColorPressed",o)]:to,[h("closeColorHover",o)]:io,[h("closeColorPressed",o)]:ho}}=c.value,I=ko(B);return{"--n-font-weight-strong":Y,"--n-avatar-size-override":`calc(${V} - 8px)`,"--n-bezier":x,"--n-border-radius":H,"--n-border":so,"--n-close-icon-size":ro,"--n-close-color-pressed":ho,"--n-close-color-hover":io,"--n-close-border-radius":G,"--n-close-icon-color":D,"--n-close-icon-color-hover":no,"--n-close-icon-color-pressed":to,"--n-close-icon-color-disabled":D,"--n-close-margin-top":I.top,"--n-close-margin-right":I.right,"--n-close-margin-bottom":I.bottom,"--n-close-margin-left":I.left,"--n-close-size":eo,"--n-color":u||(e.value?oo:lo),"--n-color-checkable":E,"--n-color-checked":O,"--n-color-checked-hover":X,"--n-color-checked-pressed":Z,"--n-color-hover-checkable":W,"--n-color-pressed-checkable":j,"--n-font-size":ao,"--n-height":V,"--n-opacity-disabled":$,"--n-padding":_,"--n-text-color":b||co,"--n-text-color-checkable":T,"--n-text-color-checked":w,"--n-text-color-hover-checkable":M,"--n-text-color-pressed-checkable":R}}),C=a?xo("tag",U(()=>{let o="";const{type:s,size:u,color:{color:b,textColor:x}={}}=l;return o+=s[0],o+=u[0],b&&(o+=`a${K(b)}`),x&&(o+=`b${K(x)}`),e.value&&(o+="c"),o}),g,l):void 0;return Object.assign(Object.assign({},m),{rtlEnabled:k,mergedClsPrefix:v,contentRef:i,mergedBordered:e,handleClick:t,handleCloseClick:d,cssVars:a?void 0:g,themeClass:C==null?void 0:C.themeClass,onRender:C==null?void 0:C.onRender})},render(){var l,i;const{mergedClsPrefix:e,rtlEnabled:v,closable:a,color:{borderColor:n}={},round:c,onRender:t,$slots:d}=this;t==null||t();const m=L(d.avatar,g=>g&&P("div",{class:`${e}-tag__avatar`},g)),k=L(d.icon,g=>g&&P("div",{class:`${e}-tag__icon`},g));return P("div",{class:[`${e}-tag`,this.themeClass,{[`${e}-tag--rtl`]:v,[`${e}-tag--strong`]:this.strong,[`${e}-tag--disabled`]:this.disabled,[`${e}-tag--checkable`]:this.checkable,[`${e}-tag--checked`]:this.checkable&&this.checked,[`${e}-tag--round`]:c,[`${e}-tag--avatar`]:m,[`${e}-tag--icon`]:k,[`${e}-tag--closable`]:a}],style:this.cssVars,onClick:this.handleClick,onMouseenter:this.onMouseenter,onMouseleave:this.onMouseleave},k||m,P("span",{class:`${e}-tag__content`,ref:"contentRef"},(i=(l=this.$slots).default)===null||i===void 0?void 0:i.call(l)),!this.checkable&&a?P(Co,{clsPrefix:e,class:`${e}-tag__close`,disabled:this.disabled,onClick:this.handleCloseClick,focusable:this.internalCloseFocusable,round:c,isButtonTag:this.internalCloseIsButtonTag,absolute:!0}):null,!this.checkable&&this.mergedBordered?P("div",{class:`${e}-tag__border`,style:{borderColor:n}}):null)}}),jo={class:"topbar"},Oo={class:"brand-block"},No={class:"version"},Fo={class:"topnav","aria-label":"Primary"},Vo=["aria-current"],Do=["aria-current"],Lo=["aria-current"],Uo=J({__name:"Topbar",props:{active:{}},setup(l){const i=F(null),e=F("version dev");Io(async()=>{try{i.value=await zo()}catch{}try{e.value=await So()}catch{}});async function v(){try{await Ho()}catch{}finally{location.assign("/login.html")}}return(a,n)=>{var c;return A(),q("header",jo,[f("div",Oo,[n[0]||(n[0]=f("div",{class:"brand"},"AT Term",-1)),f("div",No,_o(e.value),1)]),f("nav",Fo,[f("a",{href:"/",class:N({active:a.active==="home"}),"aria-current":a.active==="home"?"page":!1},"Home",10,Vo),f("a",{href:"/settings.html",class:N({active:a.active==="settings"}),"aria-current":a.active==="settings"?"page":!1},"Settings",10,Do),(c=i.value)!=null&&c.is_admin?(A(),q("a",{key:0,href:"/admin/",class:N({active:a.active==="admin"}),"aria-current":a.active==="admin"?"page":!1},"Admin",10,Lo)):Bo("",!0)]),f("button",{type:"button",class:"ghost-btn",onClick:v},"Sign out")])}}}),qo=$o(Uo,[["__scopeId","data-v-232a58ee"]]);export{Ao as N,qo as T};
