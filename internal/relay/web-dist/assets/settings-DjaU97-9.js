import{u as T,v as k,y as I,x as C,aF as oe,aG as le,ag as E,az as v,bP as V,bT as W,bW as L,aY as se,bj as de,bK as ce,Z,bX as K,a6 as ue,aD as ve,bI as fe,bH as pe,F as M,bo as h,bb as U,bd as g,a2 as $,bO as n,c1 as r,a1 as P,bJ as S,ac as s,a9 as y,B as R,a3 as N,a4 as D,bq as Y,a_ as me,A as z,ab as ge,by as he,_ as B,J as be,aZ as _e,bx as ye,bE as xe,ah as we,bc as ke,ax as Ce,h as $e,ae as Pe,e as Se,a0 as Te}from"./tokens-DETGupTO.js";import{N as ze,T as Ee}from"./Topbar-BEQ1VsTC.js";import{x as J,N as Re,c as Ne,a as q,e as De,d as O}from"./Tabs-BHCUv3AD.js";import{a as j,d as A,c as F,b as G}from"./FormItem-BwBJkGWJ.js";const Ie=T([k("list",`
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
 `,[I("show-divider",[k("list-item",[T("&:not(:last-child)",[C("divider",`
 background-color: var(--n-merged-border-color);
 `)])])]),I("clickable",[k("list-item",`
 cursor: pointer;
 `)]),I("bordered",`
 border: 1px solid var(--n-merged-border-color);
 border-radius: var(--n-border-radius);
 `),I("hoverable",[k("list-item",`
 border-radius: var(--n-border-radius);
 `,[T("&:hover",`
 background-color: var(--n-merged-color-hover);
 `,[C("divider",`
 background-color: transparent;
 `)])])]),I("bordered, hoverable",[k("list-item",`
 padding: 12px 20px;
 `),C("header, footer",`
 padding: 12px 20px;
 `)]),C("header, footer",`
 padding: 12px 0;
 box-sizing: border-box;
 transition: border-color .3s var(--n-bezier);
 `,[T("&:not(:last-child)",`
 border-bottom: 1px solid var(--n-merged-border-color);
 `)]),k("list-item",`
 position: relative;
 padding: 12px 0; 
 box-sizing: border-box;
 display: flex;
 flex-wrap: nowrap;
 align-items: center;
 transition:
 background-color .3s var(--n-bezier),
 border-color .3s var(--n-bezier);
 `,[C("prefix",`
 margin-right: 20px;
 flex: 0;
 `),C("suffix",`
 margin-left: 20px;
 flex: 0;
 `),C("main",`
 flex: 1;
 `),C("divider",`
 height: 1px;
 position: absolute;
 bottom: 0;
 left: 0;
 right: 0;
 background-color: transparent;
 transition: background-color .3s var(--n-bezier);
 pointer-events: none;
 `)])]),oe(k("list",`
 --n-merged-color-hover: var(--n-color-hover-modal);
 --n-merged-color: var(--n-color-modal);
 --n-merged-border-color: var(--n-border-color-modal);
 `)),le(k("list",`
 --n-merged-color-hover: var(--n-color-hover-popover);
 --n-merged-color: var(--n-color-popover);
 --n-merged-border-color: var(--n-border-color-popover);
 `))]),Ae=Object.assign(Object.assign({},L.props),{size:{type:String,default:"medium"},bordered:Boolean,clickable:Boolean,hoverable:Boolean,showDivider:{type:Boolean,default:!0}}),X=ue("n-list"),Q=E({name:"List",props:Ae,setup(t){const{mergedClsPrefixRef:e,inlineThemeDisabled:i,mergedRtlRef:o}=V(t),u=W("List",o,e),x=L("List","-list",Ie,se,t,e);de(X,{showDividerRef:ce(t,"showDivider"),mergedClsPrefixRef:e});const p=Z(()=>{const{common:{cubicBezierEaseInOut:l},self:{fontSize:c,textColor:a,color:f,colorModal:w,colorPopover:b,borderColor:m,borderColorModal:_,borderColorPopover:H,borderRadius:ae,colorHover:re,colorHoverModal:ne,colorHoverPopover:ie}}=x.value;return{"--n-font-size":c,"--n-bezier":l,"--n-text-color":a,"--n-color":f,"--n-border-radius":ae,"--n-border-color":m,"--n-border-color-modal":_,"--n-border-color-popover":H,"--n-color-modal":w,"--n-color-popover":b,"--n-color-hover":re,"--n-color-hover-modal":ne,"--n-color-hover-popover":ie}}),d=i?K("list",void 0,p,t):void 0;return{mergedClsPrefix:e,rtlEnabled:u,cssVars:i?void 0:p,themeClass:d==null?void 0:d.themeClass,onRender:d==null?void 0:d.onRender}},render(){var t;const{$slots:e,mergedClsPrefix:i,onRender:o}=this;return o==null||o(),v("ul",{class:[`${i}-list`,this.rtlEnabled&&`${i}-list--rtl`,this.bordered&&`${i}-list--bordered`,this.showDivider&&`${i}-list--show-divider`,this.hoverable&&`${i}-list--hoverable`,this.clickable&&`${i}-list--clickable`,this.themeClass],style:this.cssVars},e.header?v("div",{class:`${i}-list__header`},e.header()):null,(t=e.default)===null||t===void 0?void 0:t.call(e),e.footer?v("div",{class:`${i}-list__footer`},e.footer()):null)}}),ee=E({name:"ListItem",setup(){const t=ve(X,null);return t||fe("list-item","`n-list-item` must be placed in `n-list`."),{showDivider:t.showDividerRef,mergedClsPrefix:t.mergedClsPrefixRef}},render(){const{$slots:t,mergedClsPrefix:e}=this;return v("li",{class:`${e}-list-item`},t.prefix?v("div",{class:`${e}-list-item__prefix`},t.prefix()):null,t.default?v("div",{class:`${e}-list-item__main`},t):null,t.suffix?v("div",{class:`${e}-list-item__suffix`},t.suffix()):null,this.showDivider&&v("div",{class:`${e}-list-item__divider`}))}}),Be=k("thing",`
 display: flex;
 transition: color .3s var(--n-bezier);
 font-size: var(--n-font-size);
 color: var(--n-text-color);
`,[k("thing-avatar",`
 margin-right: 12px;
 margin-top: 2px;
 `),k("thing-avatar-header-wrapper",`
 display: flex;
 flex-wrap: nowrap;
 `,[k("thing-header-wrapper",`
 flex: 1;
 `)]),k("thing-main",`
 flex-grow: 1;
 `,[k("thing-header",`
 display: flex;
 margin-bottom: 4px;
 justify-content: space-between;
 align-items: center;
 `,[C("title",`
 font-size: 16px;
 font-weight: var(--n-title-font-weight);
 transition: color .3s var(--n-bezier);
 color: var(--n-title-text-color);
 `)]),C("description",[T("&:not(:last-child)",`
 margin-bottom: 4px;
 `)]),C("content",[T("&:not(:first-child)",`
 margin-top: 12px;
 `)]),C("footer",[T("&:not(:first-child)",`
 margin-top: 12px;
 `)]),C("action",[T("&:not(:first-child)",`
 margin-top: 12px;
 `)])])]),Oe=Object.assign(Object.assign({},L.props),{title:String,titleExtra:String,description:String,descriptionClass:String,descriptionStyle:[String,Object],content:String,contentClass:String,contentStyle:[String,Object],contentIndented:Boolean}),te=E({name:"Thing",props:Oe,setup(t,{slots:e}){const{mergedClsPrefixRef:i,inlineThemeDisabled:o,mergedRtlRef:u}=V(t),x=L("Thing","-thing",Be,pe,t,i),p=W("Thing",u,i),d=Z(()=>{const{self:{titleTextColor:c,textColor:a,titleFontWeight:f,fontSize:w},common:{cubicBezierEaseInOut:b}}=x.value;return{"--n-bezier":b,"--n-font-size":w,"--n-text-color":a,"--n-title-font-weight":f,"--n-title-text-color":c}}),l=o?K("thing",void 0,d,t):void 0;return()=>{var c;const{value:a}=i,f=p?p.value:!1;return(c=l==null?void 0:l.onRender)===null||c===void 0||c.call(l),v("div",{class:[`${a}-thing`,l==null?void 0:l.themeClass,f&&`${a}-thing--rtl`],style:o?void 0:d.value},e.avatar&&t.contentIndented?v("div",{class:`${a}-thing-avatar`},e.avatar()):null,v("div",{class:`${a}-thing-main`},!t.contentIndented&&(e.header||t.title||e["header-extra"]||t.titleExtra||e.avatar)?v("div",{class:`${a}-thing-avatar-header-wrapper`},e.avatar?v("div",{class:`${a}-thing-avatar`},e.avatar()):null,e.header||t.title||e["header-extra"]||t.titleExtra?v("div",{class:`${a}-thing-header-wrapper`},v("div",{class:`${a}-thing-header`},e.header||t.title?v("div",{class:`${a}-thing-header__title`},e.header?e.header():t.title):null,e["header-extra"]||t.titleExtra?v("div",{class:`${a}-thing-header__extra`},e["header-extra"]?e["header-extra"]():t.titleExtra):null),e.description||t.description?v("div",{class:[`${a}-thing-main__description`,t.descriptionClass],style:t.descriptionStyle},e.description?e.description():t.description):null):null):v(M,null,e.header||t.title||e["header-extra"]||t.titleExtra?v("div",{class:`${a}-thing-header`},e.header||t.title?v("div",{class:`${a}-thing-header__title`},e.header?e.header():t.title):null,e["header-extra"]||t.titleExtra?v("div",{class:`${a}-thing-header__extra`},e["header-extra"]?e["header-extra"]():t.titleExtra):null):null,e.description||t.description?v("div",{class:[`${a}-thing-main__description`,t.descriptionClass],style:t.descriptionStyle},e.description?e.description():t.description):null),e.default||t.content?v("div",{class:[`${a}-thing-main__content`,t.contentClass],style:t.contentStyle},e.default?e.default():t.content):null,e.footer?v("div",{class:`${a}-thing-main__footer`},e.footer()):null,e.action?v("div",{class:`${a}-thing-main__action`},e.action()):null))}}}),qe={class:"plaintext-display"},Fe={key:2,class:"empty"},Le=E({__name:"ApiTokens",setup(t){const e=h([]),i=h(""),o=h(!1),u=h(""),x=h(!0),p=J();function d(b){try{return new Date(b).toLocaleDateString()}catch{return b}}function l(b){return!b.revoked_at}async function c(){x.value=!0;try{e.value=await me()}catch(b){b instanceof z&&p.error("Failed to load tokens.")}finally{x.value=!1}}async function a(b){b.preventDefault();const m=i.value.trim();if(!(!m||o.value)){o.value=!0;try{const _=await ge(m);u.value=_.plaintext,i.value="",await c()}catch(_){_ instanceof z&&(_.code==="name_required"?p.error("Token name is required."):_.code==="invalid_request"?p.error("Please enter a valid name."):p.error("Failed to create token."))}finally{o.value=!1}}}async function f(b){try{await he(b),await c()}catch(m){m instanceof z&&p.error("Failed to revoke token.")}}async function w(){try{await navigator.clipboard.writeText(u.value),p.success("Token copied to clipboard.")}catch{p.warning("Clipboard not available — select and copy manually.")}}return U(c),(b,m)=>(g(),$(n(j),{title:"API Tokens",bordered:!1},{default:r(()=>[u.value?(g(),$(n(Re),{key:0,type:"success","show-icon":!1,class:"plaintext-alert"},{default:r(()=>[m[2]||(m[2]=P("div",{class:"plaintext-msg"},"Copy this token now — it will not be shown again.",-1)),P("code",qe,S(u.value),1),s(n(R),{size:"small",tertiary:"",class:"plaintext-copy",onClick:w},{default:r(()=>m[1]||(m[1]=[y("Copy")])),_:1})]),_:1})):N("",!0),e.value.filter(l).length>0?(g(),$(n(Q),{key:1,bordered:""},{default:r(()=>[(g(!0),D(M,null,Y(e.value.filter(l),_=>(g(),$(n(ee),{key:_.id},{suffix:r(()=>[s(n(q),{onPositiveClick:H=>f(_.id)},{trigger:r(()=>[s(n(R),{size:"small",type:"error","data-testid":`revoke-${_.id}`},{default:r(()=>m[3]||(m[3]=[y(" Revoke ")])),_:2},1032,["data-testid"])]),default:r(()=>[m[4]||(m[4]=y(" Revoke this token? This cannot be undone. "))]),_:2},1032,["onPositiveClick"])]),default:r(()=>[s(n(te),null,{header:r(()=>[y(S(_.name),1)]),description:r(()=>[P("code",null,S(_.prefix)+"…",1),y(" · created "+S(d(_.created_at)),1)]),_:2},1024)]),_:2},1024))),128))]),_:1})):x.value?N("",!0):(g(),D("p",Fe,"No tokens yet.")),P("form",{class:"create-form",onSubmit:a,autocomplete:"off"},[s(n(Ne),{wrap:!1},{default:r(()=>[s(n(A),{value:i.value,"onUpdate:value":m[0]||(m[0]=_=>i.value=_),type:"text",placeholder:"e.g. my-laptop","input-props":{required:!0,autocomplete:"off"}},null,8,["value"]),s(n(R),{type:"primary","attr-type":"submit",loading:o.value,disabled:o.value},{default:r(()=>m[5]||(m[5]=[y(" Create ")])),_:1},8,["loading","disabled"])]),_:1})],32)]),_:1}))}}),je=B(Le,[["__scopeId","data-v-07870487"]]),Me={key:0,class:"form-error",role:"alert"},Ue=E({__name:"ChangePassword",setup(t){const e=h(""),i=h(""),o=h(!1),u=h("");function x(d){if(d instanceof z){if(d.code==="current_password_wrong")return"Current password is incorrect.";if(d.code==="password_weak")return"New password must be at least 12 characters.";if(d.code==="invalid_request")return"Please check your input."}return"Password change failed. Please try again."}async function p(d){if(d.preventDefault(),!o.value){u.value="",o.value=!0;try{await be(e.value,i.value),location.assign("/login.html")}catch(l){u.value=x(l)}finally{o.value=!1}}}return(d,l)=>(g(),$(n(j),{title:"Change Password",bordered:!1},{default:r(()=>[P("form",{onSubmit:p,autocomplete:"off",novalidate:""},[s(n(G),{"label-placement":"top","require-mark-placement":"right-hanging"},{default:r(()=>[s(n(F),{label:"Current password","show-feedback":!1},{default:r(()=>[s(n(A),{value:e.value,"onUpdate:value":l[0]||(l[0]=c=>e.value=c),type:"password","show-password-on":"click","input-props":{required:!0,autocomplete:"current-password"}},null,8,["value"])]),_:1}),s(n(F),{label:"New password (min 12 characters)","show-feedback":!1},{default:r(()=>[s(n(A),{value:i.value,"onUpdate:value":l[1]||(l[1]=c=>i.value=c),type:"password","show-password-on":"click","input-props":{required:!0,autocomplete:"new-password",minlength:12}},null,8,["value"])]),_:1}),s(n(R),{type:"primary","attr-type":"submit",loading:o.value,disabled:o.value},{default:r(()=>l[2]||(l[2]=[y(" Update password ")])),_:1},8,["loading","disabled"]),u.value?(g(),D("p",Me,S(u.value),1)):N("",!0)]),_:1})],32)]),_:1}))}}),He=B(Ue,[["__scopeId","data-v-33f39658"]]),Ve={key:1,class:"empty"},We={class:"actions"},Ze=E({__name:"Sessions",setup(t){const e=h([]),i=h(!0),o=h(!1),u=J();function x(a){return a?a.includes("Firefox")?"Firefox":a.includes("Edg/")?"Edge":a.includes("Chrome")?"Chrome":a.includes("Safari")?"Safari":a.length>40?a.slice(0,40)+"…":a:"Unknown device"}function p(a){try{return new Date(a).toLocaleString()}catch{return""}}async function d(){i.value=!0;try{e.value=await _e()}catch(a){a instanceof z&&u.error("Failed to load sessions.")}finally{i.value=!1}}async function l(a){try{await ye(a),await d()}catch(f){f instanceof z&&u.error("Revoke failed.")}}async function c(){if(!o.value){o.value=!0;try{const a=await xe();u.success(`Signed out ${a.deleted} other device${a.deleted===1?"":"s"}.`),await d()}catch(a){a instanceof z&&u.error("Sign-out-others failed.")}finally{o.value=!1}}}return U(d),(a,f)=>(g(),$(n(j),{title:"Signed-in devices",bordered:!1},{default:r(()=>[f[5]||(f[5]=P("p",{class:"muted"},"Each row is a browser or PWA where this account is signed in.",-1)),e.value.length>0?(g(),$(n(Q),{key:0,bordered:""},{default:r(()=>[(g(!0),D(M,null,Y(e.value,w=>(g(),$(n(ee),{key:w.id_hash},{suffix:r(()=>[w.is_current?N("",!0):(g(),$(n(q),{key:0,onPositiveClick:b=>l(w.id_hash)},{trigger:r(()=>[s(n(R),{size:"small",type:"error","data-testid":`revoke-session-${w.id_hash}`},{default:r(()=>f[1]||(f[1]=[y(" Revoke ")])),_:2},1032,["data-testid"])]),default:r(()=>[f[2]||(f[2]=y(" Revoke this device? You'll need to sign in again on it. "))]),_:2},1032,["onPositiveClick"]))]),default:r(()=>[s(n(te),null,{header:r(()=>[y(S(x(w.user_agent))+" ",1),w.is_current?(g(),$(n(ze),{key:0,type:"success",size:"small",round:"",style:{"margin-left":"0.5rem"}},{default:r(()=>f[0]||(f[0]=[y(" this device ")])),_:1})):N("",!0)]),description:r(()=>[y(" signed in "+S(p(w.created_at))+" · "+S(w.ip_prefix||"ip unknown"),1)]),_:2},1024)]),_:2},1024))),128))]),_:1})):i.value?N("",!0):(g(),D("p",Ve,"No active sessions.")),P("div",We,[s(n(q),{onPositiveClick:c},{trigger:r(()=>[s(n(R),{type:"error",loading:o.value,disabled:o.value,"data-testid":"sign-out-others"},{default:r(()=>f[3]||(f[3]=[y(" Sign out everywhere except this device ")])),_:1},8,["loading","disabled"])]),default:r(()=>[f[4]||(f[4]=y(" Sign out every other device? They'll all need to sign in again. "))]),_:1})])]),_:1}))}}),Ke=B(Ze,[["__scopeId","data-v-52a2a654"]]),Ye={key:0,class:"form-error",role:"alert"},Je=E({__name:"DangerZone",setup(t){const e=h(""),i=h(""),o=h(!1),u=h("");function x(l){if(l instanceof z){if(l.code==="email_mismatch")return"Email doesn't match — type your exact email.";if(l.code==="password_incorrect")return"Password is incorrect.";if(l.code==="last_admin")return"You're the last admin — promote another user first.";if(l.code==="invalid_request")return"Please check your input."}return"Delete failed. Please try again."}async function p(){if(!o.value){u.value="",o.value=!0;try{await we(e.value.trim(),i.value),location.assign("/login.html")}catch(l){u.value=x(l)}finally{o.value=!1}}}function d(l){l.preventDefault()}return(l,c)=>(g(),$(n(j),{title:"Danger zone",bordered:!1,class:"danger-card"},{default:r(()=>[c[4]||(c[4]=P("p",null,` Permanently delete this account. This cannot be undone. API tokens, web sessions, and account data are removed. Invitations you've consumed stay (their "consumed by" field is cleared). `,-1)),P("form",{onSubmit:d,autocomplete:"off",novalidate:""},[s(n(G),{"label-placement":"top","require-mark-placement":"right-hanging"},{default:r(()=>[s(n(F),{label:"Confirm by typing your full email","show-feedback":!1},{default:r(()=>[s(n(A),{value:e.value,"onUpdate:value":c[0]||(c[0]=a=>e.value=a),type:"text","input-props":{type:"email",required:!0,autocomplete:"off"}},null,8,["value"])]),_:1}),s(n(F),{label:"Current password","show-feedback":!1},{default:r(()=>[s(n(A),{value:i.value,"onUpdate:value":c[1]||(c[1]=a=>i.value=a),type:"password","show-password-on":"click","input-props":{required:!0,autocomplete:"current-password"}},null,8,["value"])]),_:1}),s(n(q),{onPositiveClick:p},{trigger:r(()=>[s(n(R),{type:"error","attr-type":"button",loading:o.value,disabled:o.value,"data-testid":"delete-account-trigger"},{default:r(()=>c[2]||(c[2]=[y(" Delete my account ")])),_:1},8,["loading","disabled"])]),default:r(()=>[c[3]||(c[3]=y(" Permanently delete this account? This cannot be undone. "))]),_:1}),u.value?(g(),D("p",Ye,S(u.value),1)):N("",!0)]),_:1})],32)]),_:1}))}}),Ge=B(Je,[["__scopeId","data-v-f9c6c4b3"]]),Xe={class:"settings-page"},Qe=E({__name:"App",setup(t){const e=["api-tokens","change-password","sessions","danger"];function i(){const d=location.hash.replace(/^#/,"");return e.includes(d)?d:"api-tokens"}const o=h(i());function u(){o.value=i()}U(()=>window.addEventListener("hashchange",u)),ke(()=>window.removeEventListener("hashchange",u));function x(d){e.includes(d)&&location.hash.replace(/^#/,"")!==d&&(location.hash="#"+d)}const p=Ce();return(d,l)=>(g(),$(n(Se),{theme:n(Pe),"theme-overrides":n(p)},{default:r(()=>[s(n($e),null,{default:r(()=>[s(Ee,{active:"settings"}),P("main",Xe,[s(n(De),{value:o.value,type:"line",animated:"","onUpdate:value":x},{default:r(()=>[s(n(O),{name:"api-tokens",tab:"API Tokens"},{default:r(()=>[s(je)]),_:1}),s(n(O),{name:"change-password",tab:"Change Password"},{default:r(()=>[s(He)]),_:1}),s(n(O),{name:"sessions",tab:"Signed-in devices"},{default:r(()=>[s(Ke)]),_:1}),s(n(O),{name:"danger",tab:"Danger zone"},{default:r(()=>[s(Ge)]),_:1})]),_:1},8,["value"])])]),_:1})]),_:1},8,["theme","theme-overrides"]))}}),et=B(Qe,[["__scopeId","data-v-223e476e"]]);Te(et).mount("#app");
