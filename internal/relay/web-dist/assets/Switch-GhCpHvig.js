import{aa as fe,F as be,C as te,R as ge,aQ as pe,ai as ae,aC as s,bT as ne,b$ as O,bY as me,$ as T,a8 as C,ay as ve,aj as b,ak as we,V as ye,H as xe,w as Z,y as a,aE as N,v as K,z as v,A as ee,a$ as L,bA as B,g as Se,d as ke,bU as Ce,bs as Y,bO as $e,bo as X,c0 as Be,D as J}from"./mobile-guard-Cy2LCTJC.js";import{m as Re}from"./FormItem-DzmezOR6.js";function q(e,n=!0,o=[]){return e.forEach(t=>{if(t!==null){if(typeof t!="object"){(typeof t=="string"||typeof t=="number")&&o.push(fe(String(t)));return}if(Array.isArray(t)){q(t,n,o);return}if(t.type===be){if(t.children===null)return;Array.isArray(t.children)&&q(t.children,n,o)}else{if(t.type===te&&n)return;o.push(t)}}}),o}function ze(e,n="default",o=[]){const r=e.$slots[n];return r===void 0?o:r()}function Ve(){return ge}const Fe={self:Ve};let Q;function _e(){if(!pe)return!0;if(Q===void 0){const e=document.createElement("div");e.style.display="flex",e.style.flexDirection="column",e.style.rowGap="1px",e.appendChild(document.createElement("div")),e.appendChild(document.createElement("div")),document.body.appendChild(e);const n=e.scrollHeight===1;return document.body.removeChild(e),Q=n}return Q}const je=Object.assign(Object.assign({},O.props),{align:String,justify:{type:String,default:"start"},inline:Boolean,vertical:Boolean,reverse:Boolean,size:{type:[String,Number,Array],default:"medium"},wrapItem:{type:Boolean,default:!0},itemClass:String,itemStyle:[String,Object],wrap:{type:Boolean,default:!0},internalUseGap:{type:Boolean,default:void 0}}),Pe=ae({name:"Space",props:je,setup(e){const{mergedClsPrefixRef:n,mergedRtlRef:o}=ne(e),t=O("Space","-space",void 0,Fe,e,n),r=me("Space",o,n);return{useGap:_e(),rtlEnabled:r,mergedClsPrefix:n,margin:T(()=>{const{size:c}=e;if(Array.isArray(c))return{horizontal:c[0],vertical:c[1]};if(typeof c=="number")return{horizontal:c,vertical:c};const{self:{[C("gap",c)]:f}}=t.value,{row:d,col:w}=ve(f);return{horizontal:b(w),vertical:b(d)}})}},render(){const{vertical:e,reverse:n,align:o,inline:t,justify:r,itemClass:c,itemStyle:f,margin:d,wrap:w,mergedClsPrefix:g,rtlEnabled:y,useGap:h,wrapItem:l,internalUseGap:p}=this,u=q(ze(this),!1);if(!u.length)return null;const A=`${d.horizontal}px`,R=`${d.horizontal/2}px`,P=`${d.vertical}px`,x=`${d.vertical/2}px`,$=u.length-1,z=r.startsWith("space-");return s("div",{role:"none",class:[`${g}-space`,y&&`${g}-space--rtl`],style:{display:t?"inline-flex":"flex",flexDirection:e&&!n?"column":e&&n?"column-reverse":!e&&n?"row-reverse":"row",justifyContent:["start","end"].includes(r)?`flex-${r}`:r,flexWrap:!w||e?"nowrap":"wrap",marginTop:h||e?"":`-${x}`,marginBottom:h||e?"":`-${x}`,alignItems:o,gap:h?`${d.vertical}px ${d.horizontal}px`:""}},!l&&(h||p)?u:u.map((F,m)=>F.type===te?F:s("div",{role:"none",class:c,style:[f,{maxWidth:"100%"},h?"":e?{marginBottom:m!==$?P:""}:y?{marginLeft:z?r==="space-between"&&m===$?"":R:m!==$?A:"",marginRight:z?r==="space-between"&&m===0?"":R:"",paddingTop:x,paddingBottom:x}:{marginRight:z?r==="space-between"&&m===$?"":R:m!==$?A:"",marginLeft:z?r==="space-between"&&m===0?"":R:"",paddingTop:x,paddingBottom:x}]},F)))}});function Te(e){const{primaryColor:n,opacityDisabled:o,borderRadius:t,textColor3:r}=e;return Object.assign(Object.assign({},ye),{iconColor:r,textColor:"white",loadingColor:n,opacityDisabled:o,railColor:"rgba(0, 0, 0, .14)",railColorActive:n,buttonBoxShadow:"0 1px 4px 0 rgba(0, 0, 0, 0.3), inset 0 0 1px 0 rgba(0, 0, 0, 0.05)",buttonColor:"#FFF",railBorderRadiusSmall:t,railBorderRadiusMedium:t,railBorderRadiusLarge:t,buttonBorderRadiusSmall:t,buttonBorderRadiusMedium:t,buttonBorderRadiusLarge:t,boxShadowFocus:`0 0 0 2px ${xe(n,{alpha:.2})}`})}const Ae={common:we,self:Te},De=Z("switch",`
 height: var(--n-height);
 min-width: var(--n-width);
 vertical-align: middle;
 user-select: none;
 -webkit-user-select: none;
 display: inline-flex;
 outline: none;
 justify-content: center;
 align-items: center;
`,[a("children-placeholder",`
 height: var(--n-rail-height);
 display: flex;
 flex-direction: column;
 overflow: hidden;
 pointer-events: none;
 visibility: hidden;
 `),a("rail-placeholder",`
 display: flex;
 flex-wrap: none;
 `),a("button-placeholder",`
 width: calc(1.75 * var(--n-rail-height));
 height: var(--n-rail-height);
 `),Z("base-loading",`
 position: absolute;
 top: 50%;
 left: 50%;
 transform: translateX(-50%) translateY(-50%);
 font-size: calc(var(--n-button-width) - 4px);
 color: var(--n-loading-color);
 transition: color .3s var(--n-bezier);
 `,[N({left:"50%",top:"50%",originalTransform:"translateX(-50%) translateY(-50%)"})]),a("checked, unchecked",`
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
 `),a("checked",`
 right: 0;
 padding-right: calc(1.25 * var(--n-rail-height) - var(--n-offset));
 `),a("unchecked",`
 left: 0;
 justify-content: flex-end;
 padding-left: calc(1.25 * var(--n-rail-height) - var(--n-offset));
 `),K("&:focus",[a("rail",`
 box-shadow: var(--n-box-shadow-focus);
 `)]),v("round",[a("rail","border-radius: calc(var(--n-rail-height) / 2);",[a("button","border-radius: calc(var(--n-button-height) / 2);")])]),ee("disabled",[ee("icon",[v("rubber-band",[v("pressed",[a("rail",[a("button","max-width: var(--n-button-width-pressed);")])]),a("rail",[K("&:active",[a("button","max-width: var(--n-button-width-pressed);")])]),v("active",[v("pressed",[a("rail",[a("button","left: calc(100% - var(--n-offset) - var(--n-button-width-pressed));")])]),a("rail",[K("&:active",[a("button","left: calc(100% - var(--n-offset) - var(--n-button-width-pressed));")])])])])])]),v("active",[a("rail",[a("button","left: calc(100% - var(--n-button-width) - var(--n-offset))")])]),a("rail",`
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
 `,[a("button-icon",`
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
 `,[N()]),a("button",`
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
 `)]),v("active",[a("rail","background-color: var(--n-rail-color-active);")]),v("loading",[a("rail",`
 cursor: wait;
 `)]),v("disabled",[a("rail",`
 cursor: not-allowed;
 opacity: .5;
 `)])]),Ee=Object.assign(Object.assign({},O.props),{size:{type:String,default:"medium"},value:{type:[String,Number,Boolean],default:void 0},loading:Boolean,defaultValue:{type:[String,Number,Boolean],default:!1},disabled:{type:Boolean,default:void 0},round:{type:Boolean,default:!0},"onUpdate:value":[Function,Array],onUpdateValue:[Function,Array],checkedValue:{type:[String,Number,Boolean],default:!0},uncheckedValue:{type:[String,Number,Boolean],default:!1},railStyle:Function,rubberBand:{type:Boolean,default:!0},onChange:[Function,Array]});let j;const We=ae({name:"Switch",props:Ee,setup(e){j===void 0&&(typeof CSS<"u"?typeof CSS.supports<"u"?j=CSS.supports("width","max(1px)"):j=!1:j=!0);const{mergedClsPrefixRef:n,inlineThemeDisabled:o}=ne(e),t=O("Switch","-switch",De,Ae,e,n),r=Ce(e),{mergedSizeRef:c,mergedDisabledRef:f}=r,d=Y(e.defaultValue),w=$e(e,"value"),g=Re(w,d),y=T(()=>g.value===e.checkedValue),h=Y(!1),l=Y(!1),p=T(()=>{const{railStyle:i}=e;if(i)return i({focused:l.value,checked:y.value})});function u(i){const{"onUpdate:value":D,onChange:E,onUpdateValue:M}=e,{nTriggerFormInput:W,nTriggerFormChange:U}=r;D&&J(D,i),M&&J(M,i),E&&J(E,i),d.value=i,W(),U()}function A(){const{nTriggerFormFocus:i}=r;i()}function R(){const{nTriggerFormBlur:i}=r;i()}function P(){e.loading||f.value||(g.value!==e.checkedValue?u(e.checkedValue):u(e.uncheckedValue))}function x(){l.value=!0,A()}function $(){l.value=!1,R(),h.value=!1}function z(i){e.loading||f.value||i.key===" "&&(g.value!==e.checkedValue?u(e.checkedValue):u(e.uncheckedValue),h.value=!1)}function F(i){e.loading||f.value||i.key===" "&&(i.preventDefault(),h.value=!0)}const m=T(()=>{const{value:i}=c,{self:{opacityDisabled:D,railColor:E,railColorActive:M,buttonBoxShadow:W,buttonColor:U,boxShadowFocus:re,loadingColor:ie,textColor:oe,iconColor:le,[C("buttonHeight",i)]:S,[C("buttonWidth",i)]:se,[C("buttonWidthPressed",i)]:ce,[C("railHeight",i)]:k,[C("railWidth",i)]:_,[C("railBorderRadius",i)]:de,[C("buttonBorderRadius",i)]:ue},common:{cubicBezierEaseInOut:he}}=t.value;let G,H,I;return j?(G=`calc((${k} - ${S}) / 2)`,H=`max(${k}, ${S})`,I=`max(${_}, calc(${_} + ${S} - ${k}))`):(G=X((b(k)-b(S))/2),H=X(Math.max(b(k),b(S))),I=b(k)>b(S)?_:X(b(_)+b(S)-b(k))),{"--n-bezier":he,"--n-button-border-radius":ue,"--n-button-box-shadow":W,"--n-button-color":U,"--n-button-width":se,"--n-button-width-pressed":ce,"--n-button-height":S,"--n-height":H,"--n-offset":G,"--n-opacity-disabled":D,"--n-rail-border-radius":de,"--n-rail-color":E,"--n-rail-color-active":M,"--n-rail-height":k,"--n-rail-width":_,"--n-width":I,"--n-box-shadow-focus":re,"--n-loading-color":ie,"--n-text-color":oe,"--n-icon-color":le}}),V=o?Be("switch",T(()=>c.value[0]),m,e):void 0;return{handleClick:P,handleBlur:$,handleFocus:x,handleKeyup:z,handleKeydown:F,mergedRailStyle:p,pressed:h,mergedClsPrefix:n,mergedValue:g,checked:y,mergedDisabled:f,cssVars:o?void 0:m,themeClass:V==null?void 0:V.themeClass,onRender:V==null?void 0:V.onRender}},render(){const{mergedClsPrefix:e,mergedDisabled:n,checked:o,mergedRailStyle:t,onRender:r,$slots:c}=this;r==null||r();const{checked:f,unchecked:d,icon:w,"checked-icon":g,"unchecked-icon":y}=c,h=!(L(w)&&L(g)&&L(y));return s("div",{role:"switch","aria-checked":o,class:[`${e}-switch`,this.themeClass,h&&`${e}-switch--icon`,o&&`${e}-switch--active`,n&&`${e}-switch--disabled`,this.round&&`${e}-switch--round`,this.loading&&`${e}-switch--loading`,this.pressed&&`${e}-switch--pressed`,this.rubberBand&&`${e}-switch--rubber-band`],tabindex:this.mergedDisabled?void 0:0,style:this.cssVars,onClick:this.handleClick,onFocus:this.handleFocus,onBlur:this.handleBlur,onKeyup:this.handleKeyup,onKeydown:this.handleKeydown},s("div",{class:`${e}-switch__rail`,"aria-hidden":"true",style:t},B(f,l=>B(d,p=>l||p?s("div",{"aria-hidden":!0,class:`${e}-switch__children-placeholder`},s("div",{class:`${e}-switch__rail-placeholder`},s("div",{class:`${e}-switch__button-placeholder`}),l),s("div",{class:`${e}-switch__rail-placeholder`},s("div",{class:`${e}-switch__button-placeholder`}),p)):null)),s("div",{class:`${e}-switch__button`},B(w,l=>B(g,p=>B(y,u=>s(Se,null,{default:()=>this.loading?s(ke,{key:"loading",clsPrefix:e,strokeWidth:20}):this.checked&&(p||l)?s("div",{class:`${e}-switch__button-icon`,key:p?"checked-icon":"icon"},p||l):!this.checked&&(u||l)?s("div",{class:`${e}-switch__button-icon`,key:u?"unchecked-icon":"icon"},u||l):null})))),B(f,l=>l&&s("div",{key:"checked",class:`${e}-switch__checked`},l)),B(d,l=>l&&s("div",{key:"unchecked",class:`${e}-switch__unchecked`},l)))))}});export{Pe as N,We as a,q as f,ze as g};
