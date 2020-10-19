package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

//necesaary schema for the database
type new_meet struct {
	Meet_ID string `json:"Id"`
}
type participant struct {
	Name  string `json:"Name" bson:"name"`
	Email string `json:"Email" bson:"email"`
	RSVP  string `json:"RSVP" bson:"rsvp"`
}
type meeting struct {
	Id    primitive.ObjectID `bson:"_id"`
	Title string             `json:"Title" bson:"title"`
	Part  []participant      `json:"Participants" bson:"participants" `
	Start time.Time          `json:"Start Time" bson:"start" `
	End   time.Time          `json:"End Time" bson:"end"`
	Stamp time.Time          `bson:"stamp"`
}

type conditional_meets struct {
	Meetings []meeting `json:"meetings"`
}

type Error struct {
	StatusCode   int    `json:"status_code"`
	ErrorMessage string `json:"error_message"`
}

func invalid_request(w http.ResponseWriter, statCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	switch statCode {
	case 400:
		w.WriteHeader(http.StatusBadRequest)
	case 403:
		w.WriteHeader(http.StatusForbidden)
	case 404:
		w.WriteHeader(http.StatusNotFound)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
	err := Error{
		StatusCode:   statCode,
		ErrorMessage: message}
	json.NewEncoder(w).Encode(err)
}

func connectdb(ctx context.Context) *mongo.Collection {
	client, err := mongo.NewClient(options.Client().ApplyURI("mongodb://localhost:27017"))
	if err != nil {
		log.Fatal(err)
	}

	err = client.Connect(ctx)
	if err != nil {
		log.Fatal(err)
	}
	appointyDatabase := client.Database("appointy")
	meetingCollection := appointyDatabase.Collection("meetings")
	return meetingCollection
}

func main() {
	fmt.Println("Server is running. Make API calls now")
	http.HandleFunc("/meetings", mymeets)
	http.HandleFunc("/meeting/", getmeets)
	fmt.Println(http.ListenAndServe(":5000", nil))
}

//handling all POST requests
func mymeets(w http.ResponseWriter, r *http.Request) {
	switch r.Method {

	case "POST":

		if keys := r.URL.Query(); len(keys) != 0 {
			invalid_request(w, 400, "Invalid Method")
		} else {
			if ua := r.Header.Get("Content-Type"); ua != "application/json" {
				invalid_request(w, 400, "Invalid body")
			} else {
				var m meeting
				dec := json.NewDecoder(r.Body)
				dec.DisallowUnknownFields()
				err := dec.Decode(&m)
				if err != nil {
					invalid_request(w, 400, "Invalid info")
					return
				}
				m.Stamp = time.Now()
				m.Id = primitive.NewObjectID()
				ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
				meetingCollection := connectdb(ctx)

				//for race condition
				final_check := false

				for _, particip := range m.Part {

					var check meeting
					flag1 := true
					flag2 := true
					flag3 := true
					if err = meetingCollection.FindOne(ctx, bson.M{"start": bson.D{{"$lte", m.Start}}, "end": bson.D{{"$gt", m.Start}}, "participants.email": particip.Email}).Decode(&check); err != nil {
						flag1 = false
					}
					if err = meetingCollection.FindOne(ctx, bson.M{"start": bson.D{{"$lt", m.End}}, "end": bson.D{{"$gte", m.End}}, "participants.email": particip.Email}).Decode(&check); err != nil {
						flag2 = false
					}
					if err = meetingCollection.FindOne(ctx, bson.M{"start": bson.D{{"$gte", m.Start}}, "end": bson.D{{"$lte", m.End}}, "participants.email": particip.Email}).Decode(&check); err != nil {
						flag3 = false
					}
					if flag1 || flag2 || flag3 {
						final_check = true
					}

				}
				if final_check {
					invalid_request(w, 400, "Meetings are clashed")

				} else {
					insertResult, err := meetingCollection.InsertOne(ctx, m)
					if err != nil {
						log.Fatal(err)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					meet := new_meet{
						Meet_ID: insertResult.InsertedID.(primitive.ObjectID).Hex()}
					json.NewEncoder(w).Encode(meet)
				}

			}
		}
	case "GET":
		keys := r.URL.Query()
		switch len(keys) {
		case 0:
			invalid_request(w, 400, "Not a valid query at this end point")
		case 1:
			if email, ok := keys["participant"]; !ok || len(email[0]) < 1 {
				invalid_request(w, 400, "Not a valid query at this end point")
			} else {
				var meets []meeting
				ctx, _ := context.WithTimeout(context.Background(), 10*time.Second) //timeout
				meetingCollection := connectdb(ctx)                                 //collection meetings
				if len(email) > 1 {
					invalid_request(w, 400, "Only one participant can be queried at a time")
					return
				}
				cursor, err := meetingCollection.Find(ctx, bson.M{"participants.email": bson.M{"$eq": email[0]}})
				if err != nil {
					log.Fatal(err)
					return
				}
				if err = cursor.All(ctx, &meets); err != nil {
					log.Fatal(err)
					return
				}
				my_meets := conditional_meets{
					Meetings: meets}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(my_meets)
			}
		case 2: //getting mettings in time frame
			start, okStart := keys["start"]
			end, okEnd := keys["end"]
			if !okStart || !okEnd {
				invalid_request(w, 400, "Not a valid query at this end point")
			} else {
				start_time := start[0]
				end_time := end[0]
				start_tim, err := time.Parse(time.RFC3339, start_time)
				if err != nil {
					invalid_request(w, 400, "Please enter date and time in RFC3339 format")
					return
				}
				end_tim, err := time.Parse(time.RFC3339, end_time)
				if err != nil {
					invalid_request(w, 400, "Please enter date and time in RFC3339 format")
					return
				}
				var meets []meeting
				ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)

				meetingCollection := connectdb(ctx)
				cursor, err := meetingCollection.Find(ctx, bson.M{"start": bson.D{{"$gt", start_tim}}, "end": bson.D{{"$lt", end_tim}}})
				if err != nil {
					log.Fatal(err)
					return
				}
				if err = cursor.All(ctx, &meets); err != nil {
					log.Fatal(err)
					return
				}
				my_meets := conditional_meets{
					Meetings: meets}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(my_meets)
			}

		default:
			invalid_request(w, 400, "Not a valid query at this end point")
		}
	default:
		invalid_request(w, 403, "Not a valid method at this end point")
	}
}

//handling all GET requests
func getmeets(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		if meet_id := r.URL.Path[len("/meeting/"):]; len(meet_id) == 0 {
			invalid_request(w, 400, "Not valid")
		} else {
			id, err := primitive.ObjectIDFromHex(meet_id)
			if err != nil {
				invalid_request(w, 400, "Not valid")
				return
			}
			var meet meeting
			filter := bson.M{"_id": id}
			ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
			meetingCollection := connectdb(ctx)
			err = meetingCollection.FindOne(ctx, filter).Decode(&meet)
			if err != nil {
				invalid_request(w, 404, "Metting not found")
				return
			}
			w.Header().Set("Content-Type", "application/json")
			// fmt.Println(meet)
			json.NewEncoder(w).Encode(meet)
		}
	default:
		invalid_request(w, 403, "Not valid API call")
	}
}

/*

Create meeting

http://localhost:5000/meetings
Body to be entered in this format:


Body:-
	{
    "title": "First ever meeting",
    "participants": [
        {
            "name": "Max",
            "email": "maxy.com",
            "rsvp": "No"
        },
        {
            "name": "Alex",
            "email": "alex.com",
            "rsvp": "No"
        },
        {
            "name": "Booer",
            "email": "boom.com",
            "rsvp": "Yes"
        }
    ],
    "start_time": "2020-09-19T13:00:00Z",
    "end_time": "2020-09-19T17:00:00Z"
}


Getting meeting in time frame is as follows

localhost:5000/meetings?start=2017-07-30T12:14:00Z&end=2017-07-30T16:00:00Z

Getting participant by id is as follows

localhost:5000/meetings?participant=maxy.com


Getting meeting by ID

localhost:5000/meeting/{Object ID}

*/
