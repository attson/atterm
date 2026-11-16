import{G as a,H as p,x as re,L as s,K as d,aV as te,aW as ne,aq as E,aN as l,bW as u,cf as ae,cq as B,cn as de,a8 as k,ah as z,aJ as se,cr as le,P as ie,Q as ce,ay as h,N as be,bz as y,ae as S,ab as $,c7 as w,ce as _,F as ge,bM as fe,ch as ue,u as he,b5 as ve,bg as me}from"./useI18n-DG07UYFX.js";const pe=a([p("card",`
 font-size: var(--n-font-size);
 line-height: var(--n-line-height);
 display: flex;
 flex-direction: column;
 width: 100%;
 box-sizing: border-box;
 position: relative;
 border-radius: var(--n-border-radius);
 background-color: var(--n-color);
 color: var(--n-text-color);
 word-break: break-word;
 transition:
 color .3s var(--n-bezier),
 background-color .3s var(--n-bezier),
 box-shadow .3s var(--n-bezier),
 border-color .3s var(--n-bezier);
 `,[re({background:"var(--n-color-modal)"}),s("hoverable",[a("&:hover","box-shadow: var(--n-box-shadow);")]),s("content-segmented",[a(">",[d("content",{paddingTop:"var(--n-padding-bottom)"})])]),s("content-soft-segmented",[a(">",[d("content",`
 margin: 0 var(--n-padding-left);
 padding: var(--n-padding-bottom) 0;
 `)])]),s("footer-segmented",[a(">",[d("footer",{paddingTop:"var(--n-padding-bottom)"})])]),s("footer-soft-segmented",[a(">",[d("footer",`
 padding: var(--n-padding-bottom) 0;
 margin: 0 var(--n-padding-left);
 `)])]),a(">",[p("card-header",`
 box-sizing: border-box;
 display: flex;
 align-items: center;
 font-size: var(--n-title-font-size);
 padding:
 var(--n-padding-top)
 var(--n-padding-left)
 var(--n-padding-bottom)
 var(--n-padding-left);
 `,[d("main",`
 font-weight: var(--n-title-font-weight);
 transition: color .3s var(--n-bezier);
 flex: 1;
 min-width: 0;
 color: var(--n-title-text-color);
 `),d("extra",`
 display: flex;
 align-items: center;
 font-size: var(--n-font-size);
 font-weight: 400;
 transition: color .3s var(--n-bezier);
 color: var(--n-text-color);
 `),d("close",`
 margin: 0 0 0 8px;
 transition:
 background-color .3s var(--n-bezier),
 color .3s var(--n-bezier);
 `)]),d("action",`
 box-sizing: border-box;
 transition:
 background-color .3s var(--n-bezier),
 border-color .3s var(--n-bezier);
 background-clip: padding-box;
 background-color: var(--n-action-color);
 `),d("content","flex: 1; min-width: 0;"),d("content, footer",`
 box-sizing: border-box;
 padding: 0 var(--n-padding-left) var(--n-padding-bottom) var(--n-padding-left);
 font-size: var(--n-font-size);
 `,[a("&:first-child",{paddingTop:"var(--n-padding-bottom)"})]),d("action",`
 background-color: var(--n-action-color);
 padding: var(--n-padding-bottom) var(--n-padding-left);
 border-bottom-left-radius: var(--n-border-radius);
 border-bottom-right-radius: var(--n-border-radius);
 `)]),p("card-cover",`
 overflow: hidden;
 width: 100%;
 border-radius: var(--n-border-radius) var(--n-border-radius) 0 0;
 `,[a("img",`
 display: block;
 width: 100%;
 `)]),s("bordered",`
 border: 1px solid var(--n-border-color);
 `,[a("&:target","border-color: var(--n-color-target);")]),s("action-segmented",[a(">",[d("action",[a("&:not(:first-child)",{borderTop:"1px solid var(--n-border-color)"})])])]),s("content-segmented, content-soft-segmented",[a(">",[d("content",{transition:"border-color 0.3s var(--n-bezier)"},[a("&:not(:first-child)",{borderTop:"1px solid var(--n-border-color)"})])])]),s("footer-segmented, footer-soft-segmented",[a(">",[d("footer",{transition:"border-color 0.3s var(--n-bezier)"},[a("&:not(:first-child)",{borderTop:"1px solid var(--n-border-color)"})])])]),s("embedded",`
 background-color: var(--n-color-embedded);
 `)]),te(p("card",`
 background: var(--n-color-modal);
 `,[s("embedded",`
 background-color: var(--n-color-embedded-modal);
 `)])),ne(p("card",`
 background: var(--n-color-popover);
 `,[s("embedded",`
 background-color: var(--n-color-embedded-popover);
 `)]))]),xe={title:[String,Function],contentClass:String,contentStyle:[Object,String],headerClass:String,headerStyle:[Object,String],headerExtraClass:String,headerExtraStyle:[Object,String],footerClass:String,footerStyle:[Object,String],embedded:Boolean,segmented:{type:[Boolean,Object],default:!1},size:{type:String,default:"medium"},bordered:{type:Boolean,default:!0},closable:Boolean,hoverable:Boolean,role:String,onClose:[Function,Array],tag:{type:String,default:"div"},cover:Function,content:[String,Function],footer:Function,action:Function,headerExtra:Function},Ce=Object.assign(Object.assign({},B.props),xe),$e=E({name:"Card",props:Ce,setup(e){const b=()=>{const{onClose:n}=e;n&&ie(n)},{inlineThemeDisabled:g,mergedClsPrefixRef:o,mergedRtlRef:v}=ae(e),i=B("Card","-card",pe,ce,e,o),f=de("Card",v,o),m=k(()=>{const{size:n}=e,{self:{color:r,colorModal:c,colorTarget:C,textColor:x,titleTextColor:P,titleFontWeight:R,borderColor:T,actionColor:F,borderRadius:M,lineHeight:O,closeIconColor:L,closeIconColorHover:j,closeIconColorPressed:I,closeColorHover:N,closeColorPressed:V,closeBorderRadius:H,closeIconSize:W,closeSize:q,boxShadow:A,colorPopover:D,colorEmbedded:G,colorEmbeddedModal:K,colorEmbeddedPopover:J,[z("padding",n)]:Q,[z("fontSize",n)]:U,[z("titleFontSize",n)]:X},common:{cubicBezierEaseInOut:Y}}=i.value,{top:Z,left:ee,bottom:oe}=se(Q);return{"--n-bezier":Y,"--n-border-radius":M,"--n-color":r,"--n-color-modal":c,"--n-color-popover":D,"--n-color-embedded":G,"--n-color-embedded-modal":K,"--n-color-embedded-popover":J,"--n-color-target":C,"--n-text-color":x,"--n-line-height":O,"--n-action-color":F,"--n-title-text-color":P,"--n-title-font-weight":R,"--n-close-icon-color":L,"--n-close-icon-color-hover":j,"--n-close-icon-color-pressed":I,"--n-close-color-hover":N,"--n-close-color-pressed":V,"--n-border-color":T,"--n-box-shadow":A,"--n-padding-top":Z,"--n-padding-bottom":oe,"--n-padding-left":ee,"--n-font-size":U,"--n-title-font-size":X,"--n-close-size":q,"--n-close-icon-size":W,"--n-close-border-radius":H}}),t=g?le("card",k(()=>e.size[0]),m,e):void 0;return{rtlEnabled:f,mergedClsPrefix:o,mergedTheme:i,handleCloseClick:b,cssVars:g?void 0:m,themeClass:t==null?void 0:t.themeClass,onRender:t==null?void 0:t.onRender}},render(){const{segmented:e,bordered:b,hoverable:g,mergedClsPrefix:o,rtlEnabled:v,onRender:i,embedded:f,tag:m,$slots:t}=this;return i==null||i(),l(m,{class:[`${o}-card`,this.themeClass,f&&`${o}-card--embedded`,{[`${o}-card--rtl`]:v,[`${o}-card--content${typeof e!="boolean"&&e.content==="soft"?"-soft":""}-segmented`]:e===!0||e!==!1&&e.content,[`${o}-card--footer${typeof e!="boolean"&&e.footer==="soft"?"-soft":""}-segmented`]:e===!0||e!==!1&&e.footer,[`${o}-card--action-segmented`]:e===!0||e!==!1&&e.action,[`${o}-card--bordered`]:b,[`${o}-card--hoverable`]:g}],style:this.cssVars,role:this.role},u(t.cover,n=>{const r=this.cover?h([this.cover()]):n;return r&&l("div",{class:`${o}-card-cover`,role:"none"},r)}),u(t.header,n=>{const{title:r}=this,c=r?h(typeof r=="function"?[r()]:[r]):n;return c||this.closable?l("div",{class:[`${o}-card-header`,this.headerClass],style:this.headerStyle,role:"heading"},l("div",{class:`${o}-card-header__main`,role:"heading"},c),u(t["header-extra"],C=>{const x=this.headerExtra?h([this.headerExtra()]):C;return x&&l("div",{class:[`${o}-card-header__extra`,this.headerExtraClass],style:this.headerExtraStyle},x)}),this.closable&&l(be,{clsPrefix:o,class:`${o}-card-header__close`,onClick:this.handleCloseClick,absolute:!0})):null}),u(t.default,n=>{const{content:r}=this,c=r?h(typeof r=="function"?[r()]:[r]):n;return c&&l("div",{class:[`${o}-card__content`,this.contentClass],style:this.contentStyle,role:"none"},c)}),u(t.footer,n=>{const r=this.footer?h([this.footer()]):n;return r&&l("div",{class:[`${o}-card__footer`,this.footerClass],style:this.footerStyle,role:"none"},r)}),u(t.action,n=>{const r=this.action?h([this.action()]):n;return r&&l("div",{class:`${o}-card__action`,role:"none"},r)}))}}),ze={class:"language-select"},ye=["value"],Se=["value"],_e=E({__name:"LanguageSelect",setup(e){const{t:b,languageOptions:g,localePreference:o,setLocalePreference:v}=ue();async function i(f){await v(f.target.value)}return(f,m)=>(y(),S("label",ze,[$("span",null,w(_(b)("common.language")),1),$("select",{"data-testid":"language-select",value:_(o),onChange:i},[(y(!0),S(ge,null,fe(_(g),t=>(y(),S("option",{key:t.value,value:t.value},w(t.label),9,Se))),128))],40,ye)]))}}),we=he(_e,[["__scopeId","data-v-abd76a8e"]]);function Ee(e){return ve()?me()!==null?e==="setup"||e==="login"||e==="signup"||e==="firstrun"?(location.replace("/"),!0):!1:e==="setup"?!1:(location.replace("/setup.html"),!0):!1}export{we as L,$e as N,Ee as a};
