tell application "System Events" to tell application process "Mewla"
    set controls to entire contents of window "Settings"
    repeat with controlRef in controls
        set identifier to missing value
        try
            set identifier to value of attribute "AXIdentifier" of controlRef
        end try
        if identifier is "mac-lan-off" then
            if role of controlRef is not "AXButton" then error "mac-lan-off is not a button"
            if not (enabled of controlRef) then error "mac-lan-off is disabled"
            perform action "AXPress" of controlRef
            return "AXPress delivered to the enabled mac-lan-off button in Settings."
        end if
    end repeat
    error "Settings does not expose the mac-lan-off button"
end tell
