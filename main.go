package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/hashgraph/hedera-sdk-go"
	"github.com/joho/godotenv"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

//set some global references so we can reduce duplicate work across functions
const portToUse = "8080"

//set up references to the Operator information
var operatorAccount hedera.AccountID
var operatorPrivateKey hedera.Ed25519PrivateKey

//topic information
var topicId hedera.ConsensusTopicID
var submitPrivateKey hedera.Ed25519PrivateKey
var adminPrivateKey hedera.Ed25519PrivateKey

//message encryption key
var encryptionKey string

//mirror address location
var mirrorAddress string

//this map helps to connect everything together in the demo application. As we submit messages to consensus, we return
// the transactionID to the client side application, which then sends another web request to get the HCS processed
// message. As our subscriber receives topic updates, it begins to fill this map with the responses based on the
// transactionID. Once we have received the response for a transactionID the client is looking for, we can return the
// message data to the client and then close that connection.
var eventStore = make(map[string]string) //map[transactionId]messageDataAsJson

//The init function runs before the main function is called, and allows us to set up some default values for the demo
func init() {
	//load environment variables from the demo.env file
	err := godotenv.Load("demo.env")
	if err != nil {
		panic(fmt.Errorf("Unable to load enviroment variables from demo.env file. Error:\n%v\n", err))
	}

	//Get the Operator information that should have been set by godotenv
	OPERATOR_ID := os.Getenv("OPERATOR_ID")
	OPERATOR_KEY := os.Getenv("OPERATOR_KEY")

	//if either piece of Operator information is blank (either because its not set by godotenv or the value in the .env
	// file is blank), then throw an error
	if OPERATOR_ID == "" || OPERATOR_KEY == "" {
		panic(fmt.Errorf("Please ensure the OPERATOR_ID and OPERATOR_KEY have been updated in the demo.env file.\nOPERATOR_ID: %v\nOPERATOR_KEY: %v\n", OPERATOR_ID, OPERATOR_KEY))
	}

	//load the Operator information into a usable format (again, throwing errors if the data is incorrect / malformed)
	operatorAccount, err = hedera.AccountIDFromString(OPERATOR_ID)
	if err != nil {
		panic(fmt.Errorf("Unable to convert OPERATOR_ID in demo.env into Hedera AccountID. Please check the format in the demo.env file.\n"))
	}

	operatorPrivateKey, err = hedera.Ed25519PrivateKeyFromString(OPERATOR_KEY)
	if err != nil {
		panic(fmt.Errorf("Unable to convert OPERATOR_KEY in demo.env into Hedera Ed25519 Private Key. Please check the format in the demo.env file.\n"))
	}

	TOPIC_ID := os.Getenv("TOPIC_ID")

	if TOPIC_ID == "" {
		//if there isnt already a topic set in the demo.env file, create one to use and then save the details
		createTopic()
	} else {
		//check that the rest of the topic information that we will need such as the submit key and
		// the admin key exist
		TOPIC_SUBMIT_KEY := os.Getenv("TOPIC_SUBMIT_KEY")
		TOPIC_ADMIN_KEY := os.Getenv("TOPIC_ADMIN_KEY")

		if TOPIC_SUBMIT_KEY == "" || TOPIC_ADMIN_KEY == "" {
			panic(fmt.Errorf("Please ensure the Topic information has been set correctly, or clear the information in the demo.env file so that a new topic will be created automatically.\nTOPIC_ID: %v\nTOPIC_SUBMIT_KEY: %v\nTOPIC_ADMIN_KEY: %v\n", TOPIC_ID, TOPIC_SUBMIT_KEY, TOPIC_ADMIN_KEY))
		}

		//as all of the topic information appears to be correct, load our global variables.
		topicId, err = hedera.TopicIDFromString(TOPIC_ID)
		if err != nil {
			panic(fmt.Errorf("Unable to convert TOPIC_ID in demo.env into Hedera TopicID. Please check the format in the demo.env file.\n"))
		}

		adminPrivateKey, err = hedera.Ed25519PrivateKeyFromString(TOPIC_ADMIN_KEY)
		if err != nil {
			panic(fmt.Errorf("Unable to convert TOPIC_ADMIN_KEY in demo.env into Hedera Ed25519 Private Key. Please check the format in the demo.env file.\n"))
		}

		submitPrivateKey, err = hedera.Ed25519PrivateKeyFromString(TOPIC_SUBMIT_KEY)
		if err != nil {
			panic(fmt.Errorf("Unable to convert TOPIC_ADMIN_KEY in demo.env into Hedera Ed25519 Private Key. Please check the format in the demo.env file.\n"))
		}
	}

	//Check the mirror node address is set so we can subscribe to updates for our topic
	MIRROR_ADDRESS := os.Getenv("MIRROR_ADDR")
	if MIRROR_ADDRESS == "" {
		panic(fmt.Errorf("Please ensure the MIRROR_ADDRESS is set in the demo.env file.\n"))
	}
	mirrorAddress = MIRROR_ADDRESS

	//Finally, check the encryption key we will use to encrypt data before sending it to the Hedera Consensus Service, so
	// that the data is entered into consensus on the ledger, gaining the benefits of consensus timestamps, ordering and
	// immutability (with mirror nodes) whilst not revealing any potentially sensitive data
	ENCRYPTION_KEY := os.Getenv("TOPIC_ENCRYPTION_KEY")
	if ENCRYPTION_KEY == "" {
		panic(fmt.Errorf("Please ensure the TOPIC_ENCRYPTION_KEY is set in the demo.env file.\n"))
	}
	encryptionKey = ENCRYPTION_KEY
}

func main() {


	//set up http handlers for routes we will use in the demo
	http.HandleFunc("/", demoPageHandler)
	http.HandleFunc("/track", trackingHandler)
	http.HandleFunc("/retrieve", retrieveHandler)

	subscribeToTopicUpdates()

	fmt.Printf("Now listening on localhost:" + portToUse + "\n")
	log.Fatal(http.ListenAndServe(":" + portToUse, nil))
}

/*
	HELPER FUNCTIONS
*/

//This function is used to quickly generate a topic, and then save the details in the demo.env file for future use
func createTopic() {

	//first generate some keys to use as admin and submit keys
	adminKey, err := hedera.GenerateEd25519PrivateKey()
	if err != nil {
		panic(fmt.Errorf("Error when attempting to generate a private topic admin key. Err: %v\n", err))
	}

	submitKey, err := hedera.GenerateEd25519PrivateKey()
	if err != nil {
		panic(fmt.Errorf("Error when attempting to generate a private topic submit key. Err: %v\n", err))
	}

	//Get the *client we use to interact with the Hedera Hashgraph network
	client := hedera.ClientForTestnet()
	client.SetOperator(operatorAccount, operatorPrivateKey)

	//Build the Topic Create transaction, setting the keypairs we will use as well as some required values
	builtTxn, err := hedera.NewConsensusTopicCreateTransaction().
		SetMaxTransactionFee(hedera.HbarFromTinybar(100000000)).
		SetTopicMemo("AdsDax HCS demo topic").
		SetAdminKey(adminKey.PublicKey()).
		SetSubmitKey(submitKey.PublicKey()).
		SetAutoRenewAccountID(operatorAccount).
		SetAutoRenewPeriod(7776000 * time.Second).
		Build(client)

	if err != nil {
		panic(fmt.Errorf("Error when attempting to build HCS Topic Create transaction: %v\n", err))
	}

	//Now sign and submit the transaction as the operator (who pays for the transaction) and the admin (required)
	txnId, err := builtTxn.
		SignWith(operatorPrivateKey.PublicKey(), operatorPrivateKey.Sign).
		SignWith(adminKey.PublicKey(), adminKey.Sign).
		Execute(client)

	if err != nil {
		panic(fmt.Errorf("Error when attempting to execute HCS Topic Create transaction: %v\n", err))
	}

	receipt, err := txnId.GetReceipt(client)
	if err != nil {
		panic(fmt.Errorf("Error when retrieving receipt for transaction %v. Error: %v\n", txnId.String(), err))
	}

	if receipt.Status != hedera.StatusSuccess {
		panic(fmt.Errorf("Unable to create hedera topic (receipt shows non-Success status %v)\n", receipt.Status))
	}

	//store the variables for use on the next run. The "godotenv" package overwrites comments, so parse
	// the file line by line to write the lines in individually and preserve the comments
	writeMap := make(map[string]string)

	writeMap["TOPIC_ID"] = receipt.GetConsensusTopicID().String()
	writeMap["TOPIC_SUBMIT_KEY"] = submitKey.String()
	writeMap["TOPIC_ADMIN_KEY"] = adminKey.String()

	niceWrite(writeMap, "demo.env")

	//finally, populate the global variables with these new values for use in the rest of the application
	topicId = receipt.GetConsensusTopicID()
	adminPrivateKey = adminKey
	submitPrivateKey = submitKey
}

//This function is used to nicely write environment variables back to the .env file without losing any comments
func niceWrite (writeMap map[string]string, filepath string) {

	fileContents, err := ioutil.ReadFile(filepath)
	if err != nil {
		panic(fmt.Errorf("An error occured when attempting to open file with path (%v) for writing. Error: %v\n", filepath, err))
	}

	fileLines := strings.Split(string(fileContents), "\n")

	for lineNumber, lineContent := range fileLines {
		if len(lineContent) == 0 || string(lineContent[0]) == "#" {
			//skip any of the lines that have 0 length or start with comments
			continue
		}

		for writeKey, writeValue := range writeMap {
			writeKeyLength := len(writeKey)

			if len(lineContent) < writeKeyLength {
				//skip this write key if the length of the current line is less than that of the write key
				continue
			} else if string(lineContent[0:writeKeyLength]) == writeKey {
				//this line matches our write key, so update it with the new value
				fileLines[lineNumber] = fmt.Sprintf("%v=\"%v\"", writeKey, writeValue)
			}
		}
	}

	//merge the fileLines array using strings.Join() and add the newlines back in, then write it back to the filepath
	err = ioutil.WriteFile(filepath, []byte(strings.Join(fileLines, "\n")), 0644)
	if err != nil {
		panic(fmt.Errorf("An error occured when attempting to write environment data to file (%v).Data: %v,\n Error: %v\n", filepath, writeMap, err))
	}
}

//This function takes a string message and our encryption key and returns the AES encrypted message
func encryptText (message string, cipherKey string) []byte {

	bMessage := []byte(message)
	bKey := []byte(cipherKey)

	aesCipherBlock, err := aes.NewCipher(bKey)
	if err != nil {
		panic(fmt.Errorf("An error occured generating AES cipher with key length %v (should be 16, 24 or 32). Error: %v\n", len(bKey), err))
	}

	gcmWrapper, err := cipher.NewGCM(aesCipherBlock)
	if err != nil {
		panic(fmt.Errorf("An error occured generating GCM wrapped cipher. Error: %v\n", err))
	}

	nonce := make([]byte, gcmWrapper.NonceSize())
	_, err = io.ReadFull(rand.Reader, nonce)
	if err != nil {
		panic(fmt.Errorf("An error occured generating random nonce. Error: %v\n", err))
	}

	return gcmWrapper.Seal(nonce, nonce, bMessage, nil)
}

//This function takes an encoded byte array and encryption key and returns the unencrypted message
func decryptText (encryptedText []byte, encryptionKey string) string {

	bKey := []byte(encryptionKey)

	aesCipherBlock, err := aes.NewCipher(bKey)
	if err != nil {
		panic(fmt.Errorf("An error occured generating AES cipher with key length %v (should be 16, 24 or 32). Error: %v\n", len(bKey), err))
	}

	gcmWrapper, err := cipher.NewGCM(aesCipherBlock)
	if err != nil {
		panic(fmt.Errorf("An error occured generating GCM wrapped cipher. Error: %v\n", err))
	}

	nonceSize := gcmWrapper.NonceSize()
	if len(encryptedText) < nonceSize {
		panic(fmt.Errorf("An error decrypting text. The length of the text is too short compared to the size of the nonce\n"))
	}

	nonce, encryptedMessage := encryptedText[:nonceSize], encryptedText[nonceSize:]
	decryptedMessage, err := gcmWrapper.Open(nil, nonce, encryptedMessage, nil)
	if err != nil {
		panic(fmt.Errorf("An error occured when trying to decrypt the message. Error: %v\n", err))
	}

	return string(decryptedMessage)
}

//this function handles subscribing to our topic to receive messages as they pass through consensus. The messages
// are handed off to the hcsMessageResponseHandler function, with any errors going ot the hcsMessageErrorHandler
func subscribeToTopicUpdates() {

	//set up the client
	client := hedera.ClientForTestnet()
	client.SetOperator(operatorAccount, operatorPrivateKey)

	//get the mirror address as set in the demo.env file
	mirrorClient, err := hedera.NewMirrorClient(mirrorAddress)
	if err != nil {
		panic(err)
	}

	//Set up which topics we want to listen to and then begin listening for updates
	_, err = hedera.NewMirrorConsensusTopicQuery().
		SetTopicID(topicId).
		Subscribe(mirrorClient, hcsMessageResponseHandler, hcsMessageErrorHandler)

	/*
		NOTE:

		Depending on your application logic, you may need to prevent the goroutine from finishing, which then shuts
		down your subscriber and stops you receiving updates (this isn't necessary in the demo as the call to
		http.ListenAndServe prevents the goroutine from exiting anyway). If you do find you need to do this, you can
		simply create an infinite for loop with a call to sleep in it, which then prevents the routine from exiting,
		e.g.

		for {
			time.Sleep(100 * time.Millisecond)
		}
		fmt.Printf("You should never see this message as code following the loop won't run!")

	 */
}

//This function handles the messages our listener receives after they've been passed through the Consensus Service
func hcsMessageResponseHandler (response hedera.MirrorConsensusTopicResponse) {

	//Get additional information that the Hedera Consensus Service sends alongside our message, such as the consensus
	// timestamp and sequence number
	consensusTimestamp := response.ConsensusTimeStamp
	sequenceNumber := response.SequenceNumber
	message := string(response.Message) //The message is a byte array, so convert it into a readable string

	//As the demo messages are JSON based, we can use the Go "sjson" module to add the extra information we have
	// alongside the original message
	jsonString, err := sjson.SetRaw(
		message, //this is the JSON string we want to append data to
		"hcs", //this is the path we want to add it to, but we just want to add it to the top level of the JSON
		//below is the JSON string we want to insert
		fmt.Sprintf(`{"consensusTimestamp":%v,"consensusTimestampReadable":"%v","sequenceNumber":%v}`, consensusTimestamp.UnixNano(), consensusTimestamp.Format("2006-01-02 15:04:05.99999999"), sequenceNumber))

	if err != nil {
		panic(err)
	}

	//Now we can go about decrypting the private data we have stored in the message. First, we need to decode the
	// base64 encoding we added to the private message
	decryptionString, err := hex.DecodeString(gjson.Get(jsonString, "private").String())
	if err != nil {
		panic(err)
	}

	//decrypt the encrypted section of the message
	decryptedText := decryptText(decryptionString, encryptionKey)

	//now update our json string to replace the encrypted private section with the decrypted contents
	jsonString, err = sjson.SetRaw(jsonString, "private", decryptedText)
	if err != nil {
		panic(err)
	}

	//fetch the transaction ID from the json data and store the data in our event store so we can pass it to the client
	txnId := gjson.Get(jsonString, "public.transactionId").String()

	eventStore[txnId] = jsonString
}

//This is just a simple error handler for any errors our HCS subscriber throws. You may wish to use more complex error
// handling logic, such as restarting your subscriber or triggering an alert or notification
func hcsMessageErrorHandler (err error) {
	panic(fmt.Errorf("Received HCS subscriber error: %v\n", err))
}

/*
	STRUCTS
*/

//This is the data struct that we use to form a message before sending it to the consensus service
type HcsMessageStruct struct {
	Public struct {
		Event    	   string `json:"event"`
		Timestamp  	   string `json:"timestamp"`
		TimezoneOffset string `json:"tzOffset"`
	} `json:"public"`
	Private struct {
		AdditionalInfo   string `json:"secretMessage"`
		VideoCurrentTime string `json:"videoCurrentTime"`
		VideoDuration    string `json:"videoDuration"`
		VideoUrl	     string `json:"videoUrl"`
		UserAgent        string `json:"userAgent"`
	} `json:"private"`
}


type templateData struct {}



/*
	PAGE HANDLERS
*/

func retrieveHandler(rw http.ResponseWriter, r *http.Request) {

	//fetch the transaction ID that is attached to the request and URL decode it
	transactionId, err := url.QueryUnescape(strings.Split(r.URL.RawQuery, "?")[0])
	if err != nil {
		panic(err)
	}

	urlPrefix := fmt.Sprintf("https://explorer.kabuto.sh/testnet/topic/%v/message/", topicId.String())

	//check if the transactionId exists in our event store and return the data
	if hcsResponse, exists := eventStore[transactionId]; exists {
		sequenceNumber := gjson.Get(hcsResponse, "hcs.sequenceNumber")
		fmt.Fprint(rw, fmt.Sprintf(`{"url":"%v%v","message":%v}`, urlPrefix, sequenceNumber, hcsResponse))
	} else {
		//if the data isnt in the event store yet, then keep looking every 250ms until we find it (keeps the client
		// connection open so avoids the need for polling on the client side)
		for {
			time.Sleep(250 * time.Millisecond)
			if hcsResponse, exists := eventStore[transactionId]; exists {
				sequenceNumber := gjson.Get(hcsResponse, "hcs.sequenceNumber")
				fmt.Fprint(rw, fmt.Sprintf(`{"url":"%v%v","message":%v}`, urlPrefix, sequenceNumber, hcsResponse))
				break
			}
		}
	}
}

func trackingHandler(rw http.ResponseWriter, r *http.Request) {

	//Process the URL to get the query parameters that are sent from the client
	params, err := url.ParseQuery(strings.Split(r.URL.String(), "?")[1])
	if err != nil {
		panic(err)
	}

	//create an instance of our message struct and populate the values
	message := HcsMessageStruct{}

	//Set some "public" values (public is just the name of the field in the JSON data). These will be visible in the
	// records gathered from the mainnet and any explorers that retain the information
	message.Public.Event = params["event"][0]
	message.Public.Timestamp = params["localTimestamp"][0]
	message.Public.TimezoneOffset = params["tzOffset"][0]

	//Set some "private" values (again, this is just the name of the JSON field). This is the section we will encrypt
	// before sending our messages to the Consensus Service. This means that on both the mainnet and any explorers,
	// the message data will be stored in an encrypted format so that is it not human-readable
	message.Private.AdditionalInfo = params["additionalInfo"][0]
	message.Private.VideoCurrentTime = params["videoCT"][0]
	message.Private.VideoDuration = params["videoDuration"][0]
	message.Private.VideoUrl = params["videoUrl"][0]
	message.Private.UserAgent = params["userAgent"][0]

	//marshal the struct into a JSON string
	json, err := json.Marshal(message)
	if err != nil {
		panic(err)
	}
	jsonString := string(json)

	//encrypt the "private" field of the JSON data
	encryptedText := encryptText(gjson.Get(jsonString, "private").String(), encryptionKey)

	//replace the "private" section of the JSON data with the AES-256 encrypted data (which we additionally encode as
	// a base64 string to aid in portability and readability when trying to render the encoded message data)
	jsonString, err = sjson.Set(jsonString, "private", hex.EncodeToString(encryptedText))
	if err != nil {
		panic(err)
	}

	//Get the client so we can talk to the network
	client := hedera.ClientForTestnet()
	client.SetOperator(operatorAccount, operatorPrivateKey)

	//in order to know the transaction ID before we submit the message, we generate one, which we can then add to the
	// message itself
	txnId := hedera.NewTransactionID(operatorAccount)

	//add the transactionID to the public information
	jsonString, err = sjson.Set(jsonString, "public.transactionId", txnId.String())
	if err != nil {
		panic(err)
	}

	//build the message transaction,
	builtTxn, err := hedera.NewConsensusMessageSubmitTransaction().
		SetTopicID(topicId).
		SetMaxTransactionFee(hedera.HbarFromTinybar(100000000)).
		SetMessage([]byte(jsonString)).
		SetTransactionID(txnId).
		Build(client)

	if err != nil {
		panic(fmt.Errorf("Error when attempting to build HCS message submit transaction for topic %v: %v\n", topicId, err))
	}

	_, err = builtTxn.
		SignWith(submitPrivateKey.PublicKey(), submitPrivateKey.Sign).
		SignWith(operatorPrivateKey.PublicKey(), operatorPrivateKey.Sign).
		Execute(client)

	if err != nil {
		panic(err)
	}

	fmt.Fprint(rw, jsonString)
}

func demoPageHandler(rw http.ResponseWriter, r *http.Request) {
	t, _ := template.ParseFiles("demo.html")
	_ = t.Execute(rw, templateData{})
}