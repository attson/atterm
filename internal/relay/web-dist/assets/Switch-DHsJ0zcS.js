import{a9 as ae,T as me,_ as F,L as I,A as E,F as n,G as S,af as Ce,z as U,N as ye,b1 as we,c as xe,E as Se,W as $e,I as ze,o as ke,b3 as V,h as Re,bf as re,bp as M,bm as le,ak as Be,a2 as v,bq as se,Y as Ie,az as _e,aj as Te,a8 as $,Z as Pe,ap as ne,H as ie,aJ as q,k as Fe,d as Ve,bg as Ee,bl as Ae,aX as Z,J as Q}from"./mobile-guard-TGrfrahn.js";import{n as He,F as We,C as ce,p as te,u as l,I as je,g as H,Y as K,a9 as Le}from"./relay-config-C3hWxEH_.js";function oe(e,o=!0,a=[]){return e.forEach(t=>{if(t!==null){if(typeof t!="object"){(typeof t=="string"||typeof t=="number")&&a.push(He(String(t)));return}if(Array.isArray(t)){oe(t,o,a);return}if(t.type===We){if(t.children===null)return;Array.isArray(t.children)&&oe(t.children,o,a)}else{if(t.type===ce&&o)return;a.push(t)}}}),a}function Me(e,o="default",a=[]){const r=e.$slots[o];return r===void 0?a:r()}function Oe(e){const{lineHeight:o,borderRadius:a,fontWeightStrong:t,baseColor:r,dividerColor:u,actionColor:m,textColor1:h,textColor2:f,closeColorHover:g,closeColorPressed:p,closeIconColor:b,closeIconColorHover:s,closeIconColorPressed:d,infoColor:i,successColor:z,warningColor:y,errorColor:k,fontSize:w}=e;return Object.assign(Object.assign({},me),{fontSize:w,lineHeight:o,titleFontWeight:t,borderRadius:a,border:`1px solid ${u}`,color:m,titleTextColor:h,iconColor:f,contentTextColor:f,closeBorderRadius:a,closeColorHover:g,closeColorPressed:p,closeIconColor:b,closeIconColorHover:s,closeIconColorPressed:d,borderInfo:`1px solid ${F(r,I(i,{alpha:.25}))}`,colorInfo:F(r,I(i,{alpha:.08})),titleTextColorInfo:h,iconColorInfo:i,contentTextColorInfo:f,closeColorHoverInfo:g,closeColorPressedInfo:p,closeIconColorInfo:b,closeIconColorHoverInfo:s,closeIconColorPressedInfo:d,borderSuccess:`1px solid ${F(r,I(z,{alpha:.25}))}`,colorSuccess:F(r,I(z,{alpha:.08})),titleTextColorSuccess:h,iconColorSuccess:z,contentTextColorSuccess:f,closeColorHoverSuccess:g,closeColorPressedSuccess:p,closeIconColorSuccess:b,closeIconColorHoverSuccess:s,closeIconColorPressedSuccess:d,borderWarning:`1px solid ${F(r,I(y,{alpha:.33}))}`,colorWarning:F(r,I(y,{alpha:.08})),titleTextColorWarning:h,iconColorWarning:y,contentTextColorWarning:f,closeColorHoverWarning:g,closeColorPressedWarning:p,closeIconColorWarning:b,closeIconColorHoverWarning:s,closeIconColorPressedWarning:d,borderError:`1px solid ${F(r,I(k,{alpha:.25}))}`,colorError:F(r,I(k,{alpha:.08})),titleTextColorError:h,iconColorError:k,contentTextColorError:f,closeColorHoverError:g,closeColorPressedError:p,closeIconColorError:b,closeIconColorHoverError:s,closeIconColorPressedError:d})}const De={common:ae,self:Oe},Ge=E("alert",`
 line-height: var(--n-line-height);
 border-radius: var(--n-border-radius);
 position: relative;
 transition: background-color .3s var(--n-bezier);
 background-color: var(--n-color);
 text-align: start;
 word-break: break-word;
`,[n("border",`
 border-radius: inherit;
 position: absolute;
 left: 0;
 right: 0;
 top: 0;
 bottom: 0;
 transition: border-color .3s var(--n-bezier);
 border: var(--n-border);
 pointer-events: none;
 `),S("closable",[E("alert-body",[n("title",`
 padding-right: 24px;
 `)])]),n("icon",{color:"var(--n-icon-color)"}),E("alert-body",{padding:"var(--n-padding)"},[n("title",{color:"var(--n-title-text-color)"}),n("content",{color:"var(--n-content-text-color)"})]),Ce({originalTransition:"transform .3s var(--n-bezier)",enterToProps:{transform:"scale(1)"},leaveToProps:{transform:"scale(0.9)"}}),n("icon",`
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
 `),n("close",`
 transition:
 color .3s var(--n-bezier),
 background-color .3s var(--n-bezier);
 position: absolute;
 right: 0;
 top: 0;
 margin: var(--n-close-margin);
 `),S("show-icon",[E("alert-body",{paddingLeft:"calc(var(--n-icon-margin-left) + var(--n-icon-size) + var(--n-icon-margin-right))"})]),S("right-adjust",[E("alert-body",{paddingRight:"calc(var(--n-close-size) + var(--n-padding) + 2px)"})]),E("alert-body",`
 border-radius: var(--n-border-radius);
 transition: border-color .3s var(--n-bezier);
 `,[n("title",`
 transition: color .3s var(--n-bezier);
 font-size: 16px;
 line-height: 19px;
 font-weight: var(--n-title-font-weight);
 `,[U("& +",[n("content",{marginTop:"9px"})])]),n("content",{transition:"color .3s var(--n-bezier)",fontSize:"var(--n-font-size)"})]),n("icon",{transition:"color .3s var(--n-bezier)"})]),Ue=Object.assign(Object.assign({},M.props),{title:String,showIcon:{type:Boolean,default:!0},type:{type:String,default:"default"},bordered:{type:Boolean,default:!0},closable:Boolean,onClose:Function,onAfterLeave:Function,onAfterHide:Function}),ro=te({name:"Alert",inheritAttrs:!1,props:Ue,setup(e){const{mergedClsPrefixRef:o,mergedBorderedRef:a,inlineThemeDisabled:t,mergedRtlRef:r}=re(e),u=M("Alert","-alert",Ge,De,e,o),m=le("Alert",r,o),h=H(()=>{const{common:{cubicBezierEaseInOut:d},self:i}=u.value,{fontSize:z,borderRadius:y,titleFontWeight:k,lineHeight:w,iconSize:R,iconMargin:B,iconMarginRtl:A,closeIconSize:x,closeBorderRadius:_,closeSize:c,closeMargin:W,closeMarginRtl:j,padding:L}=i,{type:C}=e,{left:O,right:Y}=Be(B);return{"--n-bezier":d,"--n-color":i[v("color",C)],"--n-close-icon-size":x,"--n-close-border-radius":_,"--n-close-color-hover":i[v("closeColorHover",C)],"--n-close-color-pressed":i[v("closeColorPressed",C)],"--n-close-icon-color":i[v("closeIconColor",C)],"--n-close-icon-color-hover":i[v("closeIconColorHover",C)],"--n-close-icon-color-pressed":i[v("closeIconColorPressed",C)],"--n-icon-color":i[v("iconColor",C)],"--n-border":i[v("border",C)],"--n-title-text-color":i[v("titleTextColor",C)],"--n-content-text-color":i[v("contentTextColor",C)],"--n-line-height":w,"--n-border-radius":y,"--n-font-size":z,"--n-title-font-weight":k,"--n-icon-size":R,"--n-icon-margin":B,"--n-icon-margin-rtl":A,"--n-close-size":c,"--n-close-margin":W,"--n-close-margin-rtl":j,"--n-padding":L,"--n-icon-margin-left":O,"--n-icon-margin-right":Y}}),f=t?se("alert",H(()=>e.type[0]),h,e):void 0,g=K(!0),p=()=>{const{onAfterLeave:d,onAfterHide:i}=e;d&&d(),i&&i()};return{rtlEnabled:m,mergedClsPrefix:o,mergedBordered:a,visible:g,handleCloseClick:()=>{var d;Promise.resolve((d=e.onClose)===null||d===void 0?void 0:d.call(e)).then(i=>{i!==!1&&(g.value=!1)})},handleAfterLeave:()=>{p()},mergedTheme:u,cssVars:t?void 0:h,themeClass:f==null?void 0:f.themeClass,onRender:f==null?void 0:f.onRender}},render(){var e;return(e=this.onRender)===null||e===void 0||e.call(this),l(Re,{onAfterLeave:this.handleAfterLeave},{default:()=>{const{mergedClsPrefix:o,$slots:a}=this,t={class:[`${o}-alert`,this.themeClass,this.closable&&`${o}-alert--closable`,this.showIcon&&`${o}-alert--show-icon`,!this.title&&this.closable&&`${o}-alert--right-adjust`,this.rtlEnabled&&`${o}-alert--rtl`],style:this.cssVars,role:"alert"};return this.visible?l("div",Object.assign({},je(this.$attrs,t)),this.closable&&l(ye,{clsPrefix:o,class:`${o}-alert__close`,onClick:this.handleCloseClick}),this.bordered&&l("div",{class:`${o}-alert__border`}),this.showIcon&&l("div",{class:`${o}-alert__icon`,"aria-hidden":"true"},we(a.icon,()=>[l(xe,{clsPrefix:o},{default:()=>{switch(this.type){case"success":return l(ke,null);case"info":return l(ze,null);case"warning":return l($e,null);case"error":return l(Se,null);default:return null}}})])),l("div",{class:[`${o}-alert-body`,this.mergedBordered&&`${o}-alert-body--bordered`]},V(a.header,r=>{const u=r||this.title;return u?l("div",{class:`${o}-alert-body__title`},u):null}),a.default&&l("div",{class:`${o}-alert-body__content`},a))):null}})}});function Ke(){return Ie}const Ye={self:Ke};let ee;function Je(){if(!_e)return!0;if(ee===void 0){const e=document.createElement("div");e.style.display="flex",e.style.flexDirection="column",e.style.rowGap="1px",e.appendChild(document.createElement("div")),e.appendChild(document.createElement("div")),document.body.appendChild(e);const o=e.scrollHeight===1;return document.body.removeChild(e),ee=o}return ee}const Ne=Object.assign(Object.assign({},M.props),{align:String,justify:{type:String,default:"start"},inline:Boolean,vertical:Boolean,reverse:Boolean,size:{type:[String,Number,Array],default:"medium"},wrapItem:{type:Boolean,default:!0},itemClass:String,itemStyle:[String,Object],wrap:{type:Boolean,default:!0},internalUseGap:{type:Boolean,default:void 0}}),to=te({name:"Space",props:Ne,setup(e){const{mergedClsPrefixRef:o,mergedRtlRef:a}=re(e),t=M("Space","-space",void 0,Ye,e,o),r=le("Space",a,o);return{useGap:Je(),rtlEnabled:r,mergedClsPrefix:o,margin:H(()=>{const{size:u}=e;if(Array.isArray(u))return{horizontal:u[0],vertical:u[1]};if(typeof u=="number")return{horizontal:u,vertical:u};const{self:{[v("gap",u)]:m}}=t.value,{row:h,col:f}=Te(m);return{horizontal:$(f),vertical:$(h)}})}},render(){const{vertical:e,reverse:o,align:a,inline:t,justify:r,itemClass:u,itemStyle:m,margin:h,wrap:f,mergedClsPrefix:g,rtlEnabled:p,useGap:b,wrapItem:s,internalUseGap:d}=this,i=oe(Me(this),!1);if(!i.length)return null;const z=`${h.horizontal}px`,y=`${h.horizontal/2}px`,k=`${h.vertical}px`,w=`${h.vertical/2}px`,R=i.length-1,B=r.startsWith("space-");return l("div",{role:"none",class:[`${g}-space`,p&&`${g}-space--rtl`],style:{display:t?"inline-flex":"flex",flexDirection:e&&!o?"column":e&&o?"column-reverse":!e&&o?"row-reverse":"row",justifyContent:["start","end"].includes(r)?`flex-${r}`:r,flexWrap:!f||e?"nowrap":"wrap",marginTop:b||e?"":`-${w}`,marginBottom:b||e?"":`-${w}`,alignItems:a,gap:b?`${h.vertical}px ${h.horizontal}px`:""}},!s&&(b||d)?i:i.map((A,x)=>A.type===ce?A:l("div",{role:"none",class:u,style:[m,{maxWidth:"100%"},b?"":e?{marginBottom:x!==R?k:""}:p?{marginLeft:B?r==="space-between"&&x===R?"":y:x!==R?z:"",marginRight:B?r==="space-between"&&x===0?"":y:"",paddingTop:w,paddingBottom:w}:{marginRight:B?r==="space-between"&&x===R?"":y:x!==R?z:"",marginLeft:B?r==="space-between"&&x===0?"":y:"",paddingTop:w,paddingBottom:w}]},A)))}});function Xe(e){const{primaryColor:o,opacityDisabled:a,borderRadius:t,textColor3:r}=e;return Object.assign(Object.assign({},Pe),{iconColor:r,textColor:"white",loadingColor:o,opacityDisabled:a,railColor:"rgba(0, 0, 0, .14)",railColorActive:o,buttonBoxShadow:"0 1px 4px 0 rgba(0, 0, 0, 0.3), inset 0 0 1px 0 rgba(0, 0, 0, 0.05)",buttonColor:"#FFF",railBorderRadiusSmall:t,railBorderRadiusMedium:t,railBorderRadiusLarge:t,buttonBorderRadiusSmall:t,buttonBorderRadiusMedium:t,buttonBorderRadiusLarge:t,boxShadowFocus:`0 0 0 2px ${I(o,{alpha:.2})}`})}const qe={common:ae,self:Xe},Ze=E("switch",`
 height: var(--n-height);
 min-width: var(--n-width);
 vertical-align: middle;
 user-select: none;
 -webkit-user-select: none;
 display: inline-flex;
 outline: none;
 justify-content: center;
 align-items: center;
`,[n("children-placeholder",`
 height: var(--n-rail-height);
 display: flex;
 flex-direction: column;
 overflow: hidden;
 pointer-events: none;
 visibility: hidden;
 `),n("rail-placeholder",`
 display: flex;
 flex-wrap: none;
 `),n("button-placeholder",`
 width: calc(1.75 * var(--n-rail-height));
 height: var(--n-rail-height);
 `),E("base-loading",`
 position: absolute;
 top: 50%;
 left: 50%;
 transform: translateX(-50%) translateY(-50%);
 font-size: calc(var(--n-button-width) - 4px);
 color: var(--n-loading-color);
 transition: color .3s var(--n-bezier);
 `,[ne({left:"50%",top:"50%",originalTransform:"translateX(-50%) translateY(-50%)"})]),n("checked, unchecked",`
 transition: color .3s var(--n-bezier);
 color: var(--n-text-color);
 box-sizing: border-box;
 position: absolute;
 white-space: nowrap;
 top: 0;
 bottom: 0;
 display: flex;
 align-items: center;
 line-height: 1;
 `),n("checked",`
 right: 0;
 padding-right: calc(1.25 * var(--n-rail-height) - var(--n-offset));
 `),n("unchecked",`
 left: 0;
 justify-content: flex-end;
 padding-left: calc(1.25 * var(--n-rail-height) - var(--n-offset));
 `),U("&:focus",[n("rail",`
 box-shadow: var(--n-box-shadow-focus);
 `)]),S("round",[n("rail","border-radius: calc(var(--n-rail-height) / 2);",[n("button","border-radius: calc(var(--n-button-height) / 2);")])]),ie("disabled",[ie("icon",[S("rubber-band",[S("pressed",[n("rail",[n("button","max-width: var(--n-button-width-pressed);")])]),n("rail",[U("&:active",[n("button","max-width: var(--n-button-width-pressed);")])]),S("active",[S("pressed",[n("rail",[n("button","left: calc(100% - var(--n-offset) - var(--n-button-width-pressed));")])]),n("rail",[U("&:active",[n("button","left: calc(100% - var(--n-offset) - var(--n-button-width-pressed));")])])])])])]),S("active",[n("rail",[n("button","left: calc(100% - var(--n-button-width) - var(--n-offset))")])]),n("rail",`
 overflow: hidden;
 height: var(--n-rail-height);
 min-width: var(--n-rail-width);
 border-radius: var(--n-rail-border-radius);
 cursor: pointer;
 position: relative;
 transition:
 opacity .3s var(--n-bezier),
 background .3s var(--n-bezier),
 box-shadow .3s var(--n-bezier);
 background-color: var(--n-rail-color);
 `,[n("button-icon",`
 color: var(--n-icon-color);
 transition: color .3s var(--n-bezier);
 font-size: calc(var(--n-button-height) - 4px);
 position: absolute;
 left: 0;
 right: 0;
 top: 0;
 bottom: 0;
 display: flex;
 justify-content: center;
 align-items: center;
 line-height: 1;
 `,[ne()]),n("button",`
 align-items: center; 
 top: var(--n-offset);
 left: var(--n-offset);
 height: var(--n-button-height);
 width: var(--n-button-width-pressed);
 max-width: var(--n-button-width);
 border-radius: var(--n-button-border-radius);
 background-color: var(--n-button-color);
 box-shadow: var(--n-button-box-shadow);
 box-sizing: border-box;
 cursor: inherit;
 content: "";
 position: absolute;
 transition:
 background-color .3s var(--n-bezier),
 left .3s var(--n-bezier),
 opacity .3s var(--n-bezier),
 max-width .3s var(--n-bezier),
 box-shadow .3s var(--n-bezier);
 `)]),S("active",[n("rail","background-color: var(--n-rail-color-active);")]),S("loading",[n("rail",`
 cursor: wait;
 `)]),S("disabled",[n("rail",`
 cursor: not-allowed;
 opacity: .5;
 `)])]),Qe=Object.assign(Object.assign({},M.props),{size:{type:String,default:"medium"},value:{type:[String,Number,Boolean],default:void 0},loading:Boolean,defaultValue:{type:[String,Number,Boolean],default:!1},disabled:{type:Boolean,default:void 0},round:{type:Boolean,default:!0},"onUpdate:value":[Function,Array],onUpdateValue:[Function,Array],checkedValue:{type:[String,Number,Boolean],default:!0},uncheckedValue:{type:[String,Number,Boolean],default:!1},railStyle:Function,rubberBand:{type:Boolean,default:!0},onChange:[Function,Array]});let G;const no=te({name:"Switch",props:Qe,setup(e){G===void 0&&(typeof CSS<"u"?typeof CSS.supports<"u"?G=CSS.supports("width","max(1px)"):G=!1:G=!0);const{mergedClsPrefixRef:o,inlineThemeDisabled:a}=re(e),t=M("Switch","-switch",Ze,qe,e,o),r=Ee(e),{mergedSizeRef:u,mergedDisabledRef:m}=r,h=K(e.defaultValue),f=Le(e,"value"),g=Ae(f,h),p=H(()=>g.value===e.checkedValue),b=K(!1),s=K(!1),d=H(()=>{const{railStyle:c}=e;if(c)return c({focused:s.value,checked:p.value})});function i(c){const{"onUpdate:value":W,onChange:j,onUpdateValue:L}=e,{nTriggerFormInput:C,nTriggerFormChange:O}=r;W&&Q(W,c),L&&Q(L,c),j&&Q(j,c),h.value=c,C(),O()}function z(){const{nTriggerFormFocus:c}=r;c()}function y(){const{nTriggerFormBlur:c}=r;c()}function k(){e.loading||m.value||(g.value!==e.checkedValue?i(e.checkedValue):i(e.uncheckedValue))}function w(){s.value=!0,z()}function R(){s.value=!1,y(),b.value=!1}function B(c){e.loading||m.value||c.key===" "&&(g.value!==e.checkedValue?i(e.checkedValue):i(e.uncheckedValue),b.value=!1)}function A(c){e.loading||m.value||c.key===" "&&(c.preventDefault(),b.value=!0)}const x=H(()=>{const{value:c}=u,{self:{opacityDisabled:W,railColor:j,railColorActive:L,buttonBoxShadow:C,buttonColor:O,boxShadowFocus:Y,loadingColor:de,textColor:ue,iconColor:he,[v("buttonHeight",c)]:T,[v("buttonWidth",c)]:fe,[v("buttonWidthPressed",c)]:ge,[v("railHeight",c)]:P,[v("railWidth",c)]:D,[v("railBorderRadius",c)]:be,[v("buttonBorderRadius",c)]:ve},common:{cubicBezierEaseInOut:pe}}=t.value;let J,N,X;return G?(J=`calc((${P} - ${T}) / 2)`,N=`max(${P}, ${T})`,X=`max(${D}, calc(${D} + ${T} - ${P}))`):(J=Z(($(P)-$(T))/2),N=Z(Math.max($(P),$(T))),X=$(P)>$(T)?D:Z($(D)+$(T)-$(P))),{"--n-bezier":pe,"--n-button-border-radius":ve,"--n-button-box-shadow":C,"--n-button-color":O,"--n-button-width":fe,"--n-button-width-pressed":ge,"--n-button-height":T,"--n-height":N,"--n-offset":J,"--n-opacity-disabled":W,"--n-rail-border-radius":be,"--n-rail-color":j,"--n-rail-color-active":L,"--n-rail-height":P,"--n-rail-width":D,"--n-width":X,"--n-box-shadow-focus":Y,"--n-loading-color":de,"--n-text-color":ue,"--n-icon-color":he}}),_=a?se("switch",H(()=>u.value[0]),x,e):void 0;return{handleClick:k,handleBlur:R,handleFocus:w,handleKeyup:B,handleKeydown:A,mergedRailStyle:d,pressed:b,mergedClsPrefix:o,mergedValue:g,checked:p,mergedDisabled:m,cssVars:a?void 0:x,themeClass:_==null?void 0:_.themeClass,onRender:_==null?void 0:_.onRender}},render(){const{mergedClsPrefix:e,mergedDisabled:o,checked:a,mergedRailStyle:t,onRender:r,$slots:u}=this;r==null||r();const{checked:m,unchecked:h,icon:f,"checked-icon":g,"unchecked-icon":p}=u,b=!(q(f)&&q(g)&&q(p));return l("div",{role:"switch","aria-checked":a,class:[`${e}-switch`,this.themeClass,b&&`${e}-switch--icon`,a&&`${e}-switch--active`,o&&`${e}-switch--disabled`,this.round&&`${e}-switch--round`,this.loading&&`${e}-switch--loading`,this.pressed&&`${e}-switch--pressed`,this.rubberBand&&`${e}-switch--rubber-band`],tabindex:this.mergedDisabled?void 0:0,style:this.cssVars,onClick:this.handleClick,onFocus:this.handleFocus,onBlur:this.handleBlur,onKeyup:this.handleKeyup,onKeydown:this.handleKeydown},l("div",{class:`${e}-switch__rail`,"aria-hidden":"true",style:t},V(m,s=>V(h,d=>s||d?l("div",{"aria-hidden":!0,class:`${e}-switch__children-placeholder`},l("div",{class:`${e}-switch__rail-placeholder`},l("div",{class:`${e}-switch__button-placeholder`}),s),l("div",{class:`${e}-switch__rail-placeholder`},l("div",{class:`${e}-switch__button-placeholder`}),d)):null)),l("div",{class:`${e}-switch__button`},V(f,s=>V(g,d=>V(p,i=>l(Fe,null,{default:()=>this.loading?l(Ve,{key:"loading",clsPrefix:e,strokeWidth:20}):this.checked&&(d||s)?l("div",{class:`${e}-switch__button-icon`,key:d?"checked-icon":"icon"},d||s):!this.checked&&(i||s)?l("div",{class:`${e}-switch__button-icon`,key:i?"unchecked-icon":"icon"},i||s):null})))),V(m,s=>s&&l("div",{key:"checked",class:`${e}-switch__checked`},s)),V(h,s=>s&&l("div",{key:"unchecked",class:`${e}-switch__unchecked`},s)))))}});export{ro as N,to as a,no as b,oe as f,Me as g};
